package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// startupPath 从命令行里挑出「能当只读页打开」的那个路径：.stlog 包文件或工程目录。
//
// 双击文件关联、命令行带路径都走这里；挑不出来返回空（正常启动，不报错）。
// 预检复用拖放那套 probeDropPath：扩展名对但内容不是会话的文件不在这里开。
func startupPath(args []string) string {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		switch probeDropPath(abs).Kind {
		case "file", "session":
			return abs
		}
	}
	return ""
}

func main() {
	svc := NewApp()
	if p := startupPath(os.Args[1:]); p != "" {
		svc.setStartupPath(p)
		log.Printf("[startup] 启动时打开: %s", p)
	}

	wailsApp := application.New(application.Options{
		Name:        "串口调试工具",
		Description: "串口调试工具 图形界面",
		Services: []application.Service{
			application.NewService(svc),
			// 虚窗文档服务（stlogview.Store 按 id 分文档的那层壳）。
			application.NewService(svc.logview),
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
		// 单实例：再启动一次 GUI = 在**已经在跑的那个进程**里新开一个窗口，而不是
		// 起第二个进程（两个进程会各自连守护进程、各自认领会话，界面看着像两份）。
		//
		// v3 的判定与通知全在内部：命名互斥体判重（Windows 走
		// single_instance_windows.go:45），第二份进程发现已在运行就发一条
		// WM_COPYDATA 然后自己 os.Exit（application.go:204-218）。这里只需要
		// 处理「第一份收到通知之后做什么」，见 app.go 的 onSecondInstanceLaunch。
		//
		// UniqueID 按 exe 路径分组（instance.go）：开发版与部署版可以同时开着。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               guiInstanceID(),
			OnSecondInstanceLaunch: svc.onSecondInstanceLaunch,
		},
	})

	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "main",
		Title:          "串口调试工具 v" + version.Version,
		Width:          1200,
		Height:         780,
		MinWidth:       640,
		MinHeight:      480,
		Frameless:      true,
		Windows:        framelessChrome,
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

	// DevTools 不再自动弹：v2 的 -devtools 构建会自动开检查器，v3 没有等价
	// 选项，以前是启动 1.2 s 后自己调一次 —— 结果每次打开 GUI 都有一块检查器
	// 挡在前面，正常用界面时还得先关掉它。
	//
	// 现在「开发者模式」只是**可用**：build.ps1 的 dev 构建带 devtools 标签，
	// 「帮助 → 开发人员工具」和 Ctrl+Shift+F12 随时能开。确实要它自己开时，
	// 启动前设 SERIAL_DEVTOOLS=1（排查「启动早期就报错」那类问题才需要）。
	if os.Getenv("SERIAL_DEVTOOLS") == "1" {
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
