package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 资源分两层，两层目录结构完全一样：
//
//	<exe>\resources-system\   内置资源（动态开发版专用）。成品版**不带**这个目录，
//	                          内置资源那时就在 exe 里由 //go:embed 提供。
//	<exe>\resources\          外置资源（用户 / 扩展）。成品版也认。
//
// 每一层里都是同样的三棵子树：
//
//	resources\          themes\<id>\theme.json + 主题自带素材
//	                    icons\<pack>\icons.json + *.svg
//	                    i18n\<lang>.json
//
// 查找顺序：内置（resources-system） → 外置（resources） → 嵌入（embed）。
// 成品版没有 resources-system，顺序自然退化成 外置 → 嵌入，与旧行为一致。
// 回退是逐个文件/逐个主题做的，不是整层切换：内置层里只放了一个主题，其余主题
// 照样能落到外置层和嵌入层。

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

// resourceDirs 返回磁盘资源来源目录，高优先级在前，只保留真实存在的目录。
// 内置层整体排在外置层之前（即使外置层位于工作目录、内置层位于 exe 目录）。
func resourceDirs() []string {
	var sysDirs, userDirs []string
	push := func(dst *[]string, d string) {
		if dirExists(d) {
			*dst = append(*dst, d)
		}
	}
	if base := exeDir(); base != "" {
		push(&sysDirs, filepath.Join(base, "resources-system"))
		push(&userDirs, filepath.Join(base, "resources"))
	}
	if wd, err := os.Getwd(); err == nil {
		push(&sysDirs, filepath.Join(wd, "resources-system"))
		push(&userDirs, filepath.Join(wd, "resources"))
	}

	seen := make(map[string]bool, len(sysDirs)+len(userDirs))
	out := make([]string, 0, len(sysDirs)+len(userDirs))
	for _, d := range append(sysDirs, userDirs...) {
		clean := filepath.Clean(d)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

// findResource 按来源顺序找 resources/ 下的某个路径（文件或目录），返回第一个存在的。
func findResource(parts ...string) string {
	for _, base := range resourceDirs() {
		p := filepath.Join(append([]string{base}, parts...)...)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// resourcePath 是老接口，语义同 findResource。
func resourcePath(parts ...string) string { return findResource(parts...) }

// ResourceDir 返回优先级最高的磁盘资源来源；没有则空串。
func ResourceDir() string {
	dirs := resourceDirs()
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

// ResourcesDir 暴露给 app 层用。
func ResourcesDir() string { return ResourceDir() }

// UserResourcesDir 返回外置资源目录 <exe>\resources（不保证已存在）。
// 「创建示例文件 / 在资源管理器里打开」这类**写操作**都落在这里，不碰内置层。
func UserResourcesDir() string {
	if base := exeDir(); base != "" {
		return filepath.Join(base, "resources")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "resources")
	}
	return ""
}

// I18nDir 磁盘上的语言包目录（优先级最高的那一层）；没有则空串。
func I18nDir() string { return findResource("i18n") }

// I18nFile 按优先级找某个语言包文件：<资源层>/i18n/<lang>.json。
func I18nFile(lang string) (string, bool) {
	if lang == "" {
		return "", false
	}
	p := findResource("i18n", lang+".json")
	return p, p != ""
}

// I18nLangs 列出所有磁盘来源里出现过的语言包（去重、排序）。
func I18nLangs() []string {
	seen := map[string]bool{}
	var langs []string
	for _, base := range resourceDirs() {
		dir := filepath.Join(base, "i18n")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") || len(name) <= 5 {
				continue
			}
			lang := strings.TrimSuffix(name, ".json")
			if seen[lang] {
				continue
			}
			seen[lang] = true
			langs = append(langs, lang)
		}
	}
	sort.Strings(langs)
	return langs
}

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
