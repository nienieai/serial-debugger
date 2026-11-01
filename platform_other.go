//go:build !windows

package main

func openDevToolsWindow() {}

func openFolder(dir string) error { return nil }
