//go:build !windows

package main

// 非 Windows 平台没有 WS_EX_NOACTIVATE 这一套，tear-off 跟随效果按平台能力降级
// （窗口会抢焦点，跟随会在第一次移动后停住）。

import "github.com/wailsapp/wails/v3/pkg/application"

func setNoActivate(w *application.WebviewWindow, on bool) {}

// 非 Windows 上没有这套轮询跟随，调用方会拿到 (0,0)/false，跟随行为降级。
func cursorPos() (int, int) { return 0, 0 }

func leftButtonDown() bool { return false }
