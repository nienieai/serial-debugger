//go:build hotreload

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestUIScanDirStable(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	writeTestFile(t, filepath.Join(dir, "app.js"), "console.log(1)")

	a := uiScanDir(dir)
	b := uiScanDir(dir)
	if a != b {
		t.Fatalf("目录未变，快照却不同: %+v vs %+v", a, b)
	}
	if a.Files != 2 {
		t.Fatalf("文件数 = %d, 期望 2", a.Files)
	}
}

func TestUIScanDirDetectsModifiedFile(t *testing.T) {
	dir := t.TempDir()
	js := filepath.Join(dir, "app.js")
	writeTestFile(t, js, "console.log(1)")

	before := uiScanDir(dir)
	writeTestFile(t, js, "console.log(2, 'changed')")
	after := uiScanDir(dir)

	if before == after {
		t.Fatalf("文件内容已变，快照却没变: %+v", after)
	}
	if after.Bytes == before.Bytes {
		t.Fatalf("字节数应随内容变化: %d -> %d", before.Bytes, after.Bytes)
	}
}

func TestUIScanDirDetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	before := uiScanDir(dir)

	writeTestFile(t, filepath.Join(dir, "style.css"), "body{}")
	after := uiScanDir(dir)

	if before == after {
		t.Fatalf("新增文件未反映到快照: %+v", after)
	}
	if after.Files != before.Files+1 {
		t.Fatalf("文件数 %d -> %d, 期望 +1", before.Files, after.Files)
	}
}

func TestUIFrontendDirPrefersEnv(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "index.html"), "<html></html>")
	t.Setenv(uiFrontendEnv, dir)

	got, err := uiFrontendDir()
	if err != nil {
		t.Fatalf("uiFrontendDir: %v", err)
	}
	if got != dir {
		t.Fatalf("got %q, 期望 %q", got, dir)
	}
}

func TestUIFrontendDirFailsWithoutIndexHTML(t *testing.T) {
	empty := t.TempDir()
	t.Setenv(uiFrontendEnv, empty)
	// 工作目录也要换掉：仓库根目录本身有 frontend/index.html，否则会命中回退项。
	t.Chdir(t.TempDir())

	if _, err := uiFrontendDir(); err == nil {
		t.Fatal("目录里没有 index.html 时应报错，而不是静默回退")
	}
}

func TestUINoStoreSetsHeaders(t *testing.T) {
	called := false
	h := uiNoStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.js", nil))

	if !called {
		t.Fatal("中间件必须放行到下游 handler")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q", got)
	}
}

// 发布构建里这些函数必须存在且是空实现，保证 main.go 的接线代码只有一份。
// 本用例只在 hotreload 构建下有意义（验证符号存在），实际行为由 release 构建保证。
func TestUIDeviceSymbolsExist(t *testing.T) {
	if uiDevMode != true {
		t.Fatal("hotreload 构建下 uiDevMode 应为 true")
	}
	if uiAssetMiddleware() == nil {
		t.Fatal("hotreload 构建下应返回 no-store 中间件")
	}
}
