//go:build hotreload

package main

// 前端热重载（开发构建专用）。
//
// 构建方式：
//
//	wails build -devtools -tags hotreload
//
// 效果：静态资源改为从磁盘上的 frontend/ 目录读取，并轮询该目录；保存任意
// 前端文件后窗口自动整页重载，Go 侧进程与串口连接不受影响。改 CSS/JS/HTML
// 不再需要重新编译。
//
// 为什么不用 -tags dev：Wails 自己也用 `dev` 这个构建标签
// （internal/app/app_dev.go、assetserver/assetserver_dev.go、
// internal/frontend/runtime/ipc_websocket.go、devserver/...），传 -tags dev
// 会把框架切进它自己的开发模式（websocket IPC、spinner、期待 Vite devserver），
// 而本项目没有前端构建链。所以这里用私有标签 hotreload。
//
// 为什么自己发 no-store：Wails 的 assetserver 只在它自己判定“从磁盘服务”时
// （servingFromDisk）才发 Cache-Control: no-cache，我们传 os.DirFS 不会触发
// 那个标志；而它本身没有 ETag/Last-Modified，WebView2 可能一直用缓存里的旧
// 文件，表现为“改了没生效”。所以这里显式关缓存。

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// uiFrontendEnv 可用环境变量显式指定前端目录；不设时按 exe 同级、工作目录
// 顺序查找。
const uiFrontendEnv = "SERIAL_GUI_FRONTEND"

// uiPollInterval 是目录轮询间隔。保存一次文件通常落在这个量级内；
// 配合“静默一拍才重载”的去抖，连续保存多个文件只会触发一次刷新。
const uiPollInterval = 400 * time.Millisecond

// uiDevMode 告诉 main.go 打开 DevTools（仍需构建时带 -devtools）。
const uiDevMode = true

// uiAssets 把静态资源换成磁盘目录：assetserver 每个请求都会重新 Open，
// 因此重载窗口即可拿到最新文件。找不到目录时直接失败退出，绝不静默回退到
// 内嵌版本——否则会出现“改了没反应、又没有任何提示”的排查黑洞。
func uiAssets() fs.FS {
	dir, err := uiFrontendDir()
	if err != nil {
		log.Fatalf("热重载模式启动失败: %v", err)
	}
	log.Printf("[hotreload] 前端从磁盘加载: %s", dir)
	return os.DirFS(dir)
}

// uiAssetMiddleware 关掉 WebView2 的缓存。
func uiAssetMiddleware() assetserver.Middleware { return uiNoStore }

func uiNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
		h.Set("Pragma", "no-cache")
		h.Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

// uiStartHotReload 启动目录监听，变更后整页重载窗口。
//
// 去抖：发现差异时先不刷新，把这一拍当成“还在写”，等下一拍确认目录已经稳定
// （与上次记录一致）才真正重载。这样一次保存同时改动多个文件只会刷新一次。
func uiStartHotReload(ctx context.Context) {
	if ctx == nil {
		return
	}
	dir, err := uiFrontendDir()
	if err != nil {
		log.Printf("[hotreload] 监听未启动: %v", err)
		return
	}
	last := uiScanDir(dir)
	log.Printf("[hotreload] 监听 %s（%d 个文件 / %d 字节，每 %s 轮询）",
		dir, last.Files, last.Bytes, uiPollInterval)

	go func() {
		ticker := time.NewTicker(uiPollInterval)
		defer ticker.Stop()
		pending := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := uiScanDir(dir)
				if cur != last {
					last = cur
					pending = true
					continue
				}
				if pending {
					pending = false
					log.Printf("[hotreload] 前端已变更，重载窗口")
					runtime.WindowReloadApp(ctx)
				}
			}
		}
	}()
}

// uiSignature 是目录快照，可直接用 != 比较。
type uiSignature struct {
	Files  int
	Bytes  int64
	Newest int64
	Sum    uint64
}

// uiScanDir 遍历目录算出快照。逐文件出错（编辑器临时文件消失、权限等）不计入
// 快照也不中断遍历——热重载的目标是“别挡路”，不是做完整性校验。
func uiScanDir(dir string) uiSignature {
	sig := uiSignature{}
	h := fnv.New64a()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sig.Files++
		sig.Bytes += info.Size()
		mt := info.ModTime().UnixNano()
		if mt > sig.Newest {
			sig.Newest = mt
		}
		_, _ = fmt.Fprintf(h, "%s|%d|%d\n", path, info.Size(), mt)
		return nil
	})
	sig.Sum = h.Sum64()
	return sig
}

// uiFrontendDir 按 SERIAL_GUI_FRONTEND → exe 同级 frontend → 工作目录 frontend
// 的顺序找带 index.html 的目录。
func uiFrontendDir() (string, error) {
	var candidates []string
	if v := os.Getenv(uiFrontendEnv); v != "" {
		candidates = append(candidates, v)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "frontend"))
		// `wails build` 的产物在 build/bin/ 下，仓库根目录是它的上两级；
		// 这样直接双击开发版 exe 也能找到前端，不必先设环境变量。
		candidates = append(candidates, filepath.Join(exeDir, "..", "..", "frontend"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "frontend"))
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("找不到前端目录（需含 index.html），已尝试: %v；可设 %s 显式指定",
		candidates, uiFrontendEnv)
}
