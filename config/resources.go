package config

import (
	"os"
	"path/filepath"
)

// resourcesDir 返回资源根目录：exe 同级的 resources\（找不到再用工作目录下的）。
//
// 里面是和外置资源一一对应的布局，磁盘上有的覆盖嵌入的：
//
//	resources\
//	  themes\<id>\theme.json  + 主题自带素材（背景图等）
//	  icons\<pack>\icons.json + *.svg
//	  i18n\<lang>.json
func resourcesDir() string {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "resources")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	if wd, err := os.Getwd(); err == nil {
		d := filepath.Join(wd, "resources")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

// resourcePath 拼一个资源路径；没有 resources 目录时返回空串。
func resourcePath(parts ...string) string {
	rd := resourcesDir()
	if rd == "" {
		return ""
	}
	return filepath.Join(append([]string{rd}, parts...)...)
}

// ResourcesDir 暴露给 app 层用（app.go 需要判断语言包目录）。
func ResourcesDir() string { return resourcesDir() }

// I18nDir 磁盘上的语言包目录：resources/i18n。
func I18nDir() string { return resourcePath("i18n") }

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
