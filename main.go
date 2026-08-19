package main

import (
	"embed"
	"log"

	"serial-tool-v3/version"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:          "串口调试工具 v" + version.Version,
		Width:          1200,
		Height:         780,
		MinWidth:       640,
		MinHeight:      480,
		Debug: options.Debug{
			OpenInspectorOnStartup: false,
		},
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []any{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
