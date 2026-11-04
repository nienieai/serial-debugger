//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// 非 Windows 平台没有 WM_NCHITTEST 这回事，热区由各平台的窗口系统管，
// 这里留空实现让调用点不用分平台写。
func installNarrowResizeEdge(w *application.WebviewWindow) {}

func forgetNarrowResizeEdge(w *application.WebviewWindow) {}
