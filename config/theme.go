package config

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 主题 = 一个文件夹：<资源层>/themes/<id>/theme.json，自带素材（背景图、缩略图等）放同一目录。
// 资源层有两份（见 resources.go）：内置 resources-system、外置 resources；
// 都没有时才用嵌入的那份。<exe>/themes/ 这个旧布局已经不读了。
//
//go:embed resources/themes/*/*
var themesFS embed.FS

const themesEmbedRoot = "resources/themes"

type ThemeInfo struct {
	ID    string   `json:"id"`
	Name  any      `json:"_name"`
	Modes []string `json:"_modes"`
}

// ExternalThemeInfo 保留给旧的绑定用；统一布局后没有「外置主题」这层概念了。
type ExternalThemeInfo struct {
	ID       string   `json:"id"`
	Name     any      `json:"name"`
	Modes    []string `json:"modes"`
	Fallback string   `json:"fallback"`
}

// themeIDs 列出所有主题 id（= 文件夹名）：按资源层优先级收集（内置 → 外置 → 嵌入），
// 同名只留优先级最高的那个。
func themeIDs() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	for _, base := range resourceDirs() {
		dir := filepath.Join(base, "themes")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && fileExists(filepath.Join(dir, e.Name(), "theme.json")) {
				add(e.Name())
			}
		}
	}
	if entries, err := fs.ReadDir(themesFS, themesEmbedRoot); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := themesFS.ReadFile(themesEmbedRoot + "/" + e.Name() + "/theme.json"); err == nil {
				add(e.Name())
			}
		}
	}
	return ids
}

// readThemeJSON 读主题定义：按资源层优先级找，最后回退嵌入。
func readThemeJSON(id string) (map[string]any, bool) {
	if p := findResource("themes", id, "theme.json"); fileExists(p) {
		if data, err := os.ReadFile(p); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				return m, true
			}
		}
	}
	data, err := themesFS.ReadFile(themesEmbedRoot + "/" + id + "/theme.json")
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil, false
	}
	return m, true
}

// readThemeAsset 读主题文件夹里的素材（缩略图、背景图）：按资源层优先级找，最后回退嵌入。
func readThemeAsset(id, name string) ([]byte, string, bool) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return nil, "", false
	}
	if p := findResource("themes", id, clean); fileExists(p) {
		if data, err := os.ReadFile(p); err == nil {
			return data, p, true
		}
	}
	data, err := themesFS.ReadFile(themesEmbedRoot + "/" + id + "/" + filepath.ToSlash(clean))
	if err != nil {
		return nil, "", false
	}
	return data, clean, true
}

// LoadTheme 读一个主题，并把主题文件夹里的图片内联成 data URI 后再返回。
func LoadTheme(id string) map[string]any {
	m, ok := readThemeJSON(id)
	if !ok {
		return nil
	}
	return inlineThemeImages(m, id)
}

func ListThemeIDs() []string {
	return themeIDs()
}

func ListThemeInfos() []ThemeInfo {
	var infos []ThemeInfo
	for _, id := range themeIDs() {
		m, ok := readThemeJSON(id)
		if !ok {
			continue
		}
		info := ThemeInfo{ID: id}
		info.Name = m["_name"]
		info.Modes = stringSlice(m["_modes"])
		infos = append(infos, info)
	}
	sort.SliceStable(infos, func(i, j int) bool {
		if (infos[i].ID == "system") != (infos[j].ID == "system") {
			return infos[i].ID == "system"
		}
		return infos[i].ID < infos[j].ID
	})
	return infos
}

// ListExternalColorThemes / LoadExternalColorTheme：统一布局后所有主题都从
// resources/themes 读，这两个绑定保持存在只是为了不重新生成前端绑定代码。
func ListExternalColorThemes() []ExternalThemeInfo { return nil }
func LoadExternalColorTheme(string) map[string]any { return nil }

// ListExternalIconThemes / LoadExternalIconTheme 同理：图标包也全部在 resources/icons 下。
func ListExternalIconThemes() []ExternalThemeInfo { return nil }
func LoadExternalIconTheme(string) map[string]any { return nil }

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok2 := it.(string); ok2 {
			out = append(out, s)
		}
	}
	return out
}

// inlineThemeImages 把主题里 url(...) 引用的图片读出来内联成 data URI。
//
// 为什么要这么做：前端由 Wails 的 asset server 提供，只挂载 frontend 目录，
// CSS 里写 `url(../themes/...)` 出不去这个根，直接 404；所以图片只能由 Go 读文件、
// 转成 data URI 塞进变量。路径相对主题自己的文件夹，写 `url(bg.jpg)` 即可，
// 内置主题的素材也在同一目录（随 //go:embed 一起进 exe）。
func inlineThemeImages(m map[string]any, id string) map[string]any {
	if m == nil || id == "" {
		return m
	}
	for _, name := range []string{"common", "dark", "light"} {
		sec, ok := m[name].(map[string]any)
		if !ok {
			continue
		}
		for k, v := range sec {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if out, ok2 := inlineCSSURL(s, id); ok2 {
				sec[k] = out
			}
		}
	}
	return m
}

func inlineCSSURL(s, id string) (string, bool) {
	val := strings.TrimSpace(s)
	if !strings.HasPrefix(val, "url(") || !strings.HasSuffix(val, ")") {
		return s, false
	}
	inner := strings.Trim(strings.TrimSpace(val[4:len(val)-1]), `'"`)
	if inner == "" || strings.Contains(inner, "://") ||
		strings.HasPrefix(inner, "data:") || strings.HasPrefix(inner, "/") {
		return s, false
	}
	// 1) 主题文件夹里的素材（磁盘或嵌入）
	if data, p, ok := readThemeAsset(id, inner); ok && len(data) > 0 && len(data) <= 8<<20 {
		return "url(data:" + themeImageMime(p) + ";base64," + base64.StdEncoding.EncodeToString(data) + ")", true
	}
	// 2) 兼容：按文件名去主题目录找、按 exe 根目录解析
	cands := []string{filepath.Join(resourcePath("themes", id), filepath.Base(inner))}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), filepath.FromSlash(inner)))
	}
	for _, cand := range cands {
		data, err := os.ReadFile(cand)
		if err != nil || len(data) == 0 || len(data) > 8<<20 {
			continue
		}
		return "url(data:" + themeImageMime(cand) + ";base64," + base64.StdEncoding.EncodeToString(data) + ")", true
	}
	return s, false
}

func themeImageMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	}
	return "application/octet-stream"
}
