package main

import (
	"embed"
	"log"
	"os"
	"time"

	"github.com/nienieai/serial-debugger/version"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend
var assets embed.FS

// 窗口/任务栏图标。
//
// v3 跟 v2 不一样：exe 文件图标（winres 的 RT_GROUP_ICON）只决定资源管理器里
// 那个图标，窗口和任务栏图标要单独给 application.Options.Icon。
//
// 这里的取值路径（见 v3 的 webview_window_windows.go:623-638）：
//  1. 先试 exe 资源里 **ID 为 3** 的图标 —— go-winres 生成的资源名是 "APP"，
//     不是数字 3，所以这条路必然失败（这就是为什么只配 winres 图标时窗口显示的是
//     Wails 默认的 W）；
//  2. 失败后回退到 application.Options.Icon。
//
// 必须用 ICO 而不是 PNG：w32.iconResourceData 对 PNG 是原样透传，1024×1024 的
// PNG 交给 CreateIconFromResourceEx 画 32×32 会失败；ICO 则会被 selectICOImage
// 挑出合适尺寸那一帧。
//
//go:embed assets/icon-gui.ico
var appIcon []byte

func main() {
	svc := NewApp()

	wailsApp := application.New(application.Options{
		Name:        "串口调试工具",
		Description: "串口调试工具 图形界面",
		Services: []application.Service{
			application.NewService(svc),
		},
		// 必须用 BundledAssetFileServer 而不是 AssetFileServerFS：
		// bindings 生成物里写死了 `import ... from "/wails/runtime.js"`，
		// 只有 Bundled 版本会在该路径提供运行时；用 AssetFileServerFS 会 404，
		// 整个前端白屏（spike 实测）。
		Assets: application.AssetOptions{
			Handler:    application.BundledAssetFileServer(uiAssets()),
			Middleware: uiAssetMiddleware(),
		},
		Icon: appIcon,
	})

	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "main",
		Title:          "串口调试工具 v" + version.Version,
		Width:          1200,
		Height:         780,
		MinWidth:       640,
		MinHeight:      480,
		EnableFileDrop: true,
		URL:            "/",
	})

	svc.attach(wailsApp, win)
	svc.setupStreams()

	// 拖入 .stlog：v3 由 Go 侧收原生 drop 事件再转给前端。
	// v2 的等价物是前端 runtime.OnFileDrop，v3 没有对应接口。
	win.OnWindowEvent(events.Common.WindowFilesDropped, svc.onFilesDroppedFor("main"))

	// 关窗口前先问一句「要不要一起停守护进程」（设置里可选三种模式）。
	//
	// 必须用 RegisterHook 而不是 OnWindowEvent：
	//   - v3 在窗口创建时就挂了一个 OnWindowEvent(WindowClosing) 监听器，
	//     里面无条件 destroy + close（webview_window.go:367-378），
	//     event.Cancel() 对它无效；
	//   - 只有 Hook 会先于监听器执行，且事件被 Cancel 后**不再派发给监听器**
	//     （events.go:151-160 的 dispatch 顺序）。
	// 用 OnWindowEvent 的症状是：确认框根本不弹、窗口直接关掉。
	//
	// 「关窗口 = 销毁本窗口的会话」这件事对每个窗口都生效，所以后续新建的窗口
	// 在 createWindowNamed 里各自注册一份（v3 的 WindowEvent 不带窗口，
	// 只能按注册时绑定的名字区分）。
	win.RegisterHook(events.Common.WindowClosing, svc.onWindowClosingFor("main"))

	uiStartHotReload(win)

	// v2 的 -devtools 构建会自动开检查器，v3 没有等价选项，
	// 等页面加载完再手动开（太早开会在空白页上开一个空面板）。
	if uiDevMode {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			win.OpenDevTools()
		}()
	}

	// 多窗口原型开关：SERIAL_PROBE_WINDOW=1 时启动后自动开一个验证窗口。
	// 窗口管理器要等 app.Run() 之后才就绪，所以这里用 goroutine + 延时。
	// 阶段 2 做出正式的窗口菜单后删掉这个开关。
	if os.Getenv("SERIAL_PROBE_WINDOW") == "1" {
		go func() {
			time.Sleep(2500 * time.Millisecond)
			log.Printf("[probe] 自动打开验证窗口: %s", svc.OpenProbeWindow())
		}()
	}

	// 会话窗口原型：SERIAL_SESSION_WINDOW=1 时把第一个会话开成独立窗口。
	if os.Getenv("SERIAL_SESSION_WINDOW") == "1" {
		go func() {
			time.Sleep(3000 * time.Millisecond)
			if sid := svc.FirstSessionID(); sid != "" {
				log.Printf("[probe] 会话窗口已打开: %s", svc.OpenSessionWindow(sid))
			} else {
				log.Printf("[probe] 守护进程上没有可用会话")
			}
		}()
	}

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
