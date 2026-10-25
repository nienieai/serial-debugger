//go:build !hotreload

package main

import (
	"context"
	"io/fs"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// 本文件是发布构建（不带 -tags hotreload）的默认实现：行为与引入热重载之前
// 完全一致，且 hotreload 那一整套代码根本不参与编译。

// uiAssets 返回交给 Wails 的静态资源。发布构建直接复用 main.go 里 embed 进去的
// 前端，exe 仍然自包含、可单文件分发。
func uiAssets() fs.FS { return assets }

// uiDevMode 告诉 main.go 是否要自动打开 DevTools。
const uiDevMode = false

// uiAssetMiddleware 在发布构建里不装任何中间件（不额外改写响应头）。
func uiAssetMiddleware() assetserver.Middleware { return nil }

// uiStartHotReload 在发布构建里是空实现。热重载只在 -tags hotreload 时存在，
// 这样发布二进制里连监听文件、重载窗口这些字符串都不会出现。
func uiStartHotReload(ctx context.Context) {}
