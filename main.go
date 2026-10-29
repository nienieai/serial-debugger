package main

import (
	"context"
	"embed"
	"log"

	"github.com/nienieai/serial-debugger/version"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "串口调试工具 v" + version.Version,
		Width:     1200,
		Height:    780,
		MinWidth:  640,
		MinHeight: 480,
		Debug: options.Debug{
			OpenInspectorOnStartup: uiDevMode,
		},
		EnableDefaultContextMenu: true,
		// 拖入 .stlog 由前端 runtime.OnFileDrop 处理（见 frontend/app.js 末尾）。
		//
		// DisableWebViewDrop 必须保持 false。它在 Windows 上的实现是 WebView2 的
		// AllowExternalDrag(false)，禁掉的是**整个**外部拖放——不是"禁用默认行为"，
		// 而是连 drop 事件都送不到页面（实测：文件拖进去毫无反应）。
		// WebView 的默认导航行为（把拖入的文件当下载）改由前端 capture 阶段的
		// preventDefault 拦住，那条路不依赖 WebView2 的拖放开关。
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		AssetServer: &assetserver.Options{
			Assets:     uiAssets(),
			Middleware: uiAssetMiddleware(),
		},
		OnStartup: func(ctx context.Context) {
			app.Startup(ctx)
			uiStartHotReload(ctx)
		},
		OnShutdown: app.Shutdown,
		Bind: []any{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
