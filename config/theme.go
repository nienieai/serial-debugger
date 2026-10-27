package config

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

//go:embed themes/*.json
var themesFS embed.FS

type ThemeInfo struct {
	ID    string   `json:"id"`
	Name  any      `json:"_name"`
	Modes []string `json:"_modes"`
}

type ExternalThemeInfo struct {
	ID       string   `json:"id"`
	Name     any      `json:"name"`
	Modes    []string `json:"modes"`
	Fallback string   `json:"fallback"`
}

func themeDir() string {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "themes")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	if wd, err := os.Getwd(); err == nil {
		d := filepath.Join(wd, "themes")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

// builtinDir 返回 exe 同级的 builtin/ 目录（也可用工作目录下的）。
// 里面按 themes/、icons/、i18n/ 分子目录，放着就从磁盘读，覆盖编译进二进制的版本。
func builtinDir() string {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "builtin")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	if wd, err := os.Getwd(); err == nil {
		d := filepath.Join(wd, "builtin")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return ""
}

func LoadTheme(id string) map[string]any {
	// 磁盘上的 builtin/themes/<id>.json 优先，方便调试和替换
	if bd := builtinDir(); bd != "" {
		if data, err := os.ReadFile(filepath.Join(bd, "themes", id+".json")); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				return m
			}
		}
	}
	data, err := themesFS.ReadFile("themes/" + id + ".json")
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

func ListThemeIDs() []string {
	seen := map[string]bool{}
	var ids []string
	// 磁盘 builtin/themes/ 里的先来，再补 embed 里没有的
	if bd := builtinDir(); bd != "" {
		if entries, err := os.ReadDir(filepath.Join(bd, "themes")); err == nil {
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !strings.HasSuffix(name, ".json") {
					continue
				}
				id := strings.TrimSuffix(name, ".json")
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	entries, err := themesFS.ReadDir("themes")
	if err != nil {
		return ids
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			id := name[:len(name)-5]
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func ListThemeInfos() []ThemeInfo {
	entries, err := themesFS.ReadDir("themes")
	if err != nil {
		return nil
	}
	var infos []ThemeInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= 5 || name[len(name)-5:] != ".json" {
			continue
		}
		id := name[:len(name)-5]
		data, err := themesFS.ReadFile("themes/" + name)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		info := ThemeInfo{ID: id}
		if v, ok := m["_id"].(string); ok && v != "" {
			info.ID = v
		}
		info.Name = m["_name"]
		if v, ok := m["_modes"].([]any); ok {
			for _, mv := range v {
				if s, ok2 := mv.(string); ok2 {
					info.Modes = append(info.Modes, s)
				}
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func ListExternalColorThemes() []ExternalThemeInfo {
	return listExternalThemes(filepath.Join(themeDir(), "colors"))
}

func LoadExternalColorTheme(id string) map[string]any {
	dir := filepath.Join(themeDir(), "colors")
	return inlineThemeImages(loadExternalThemeJSON(filepath.Join(dir, id+".json")), dir)
}

// inlineThemeImages 把主题里 url(...) 引用的图片读出来内联成 data URI。
//
// 为什么要这么做：前端（dev 热重载和打包后一样）由 Wails 的 asset server 提供，
// 只挂载 frontend 目录，CSS 里写 `url(../themes/colors/x.jpg)` 出不去这个根，直接 404；
// 所以背景图只能由 Go 读文件、转成 data URI 塞进变量里。
func inlineThemeImages(m map[string]any, baseDir string) map[string]any {
	if m == nil {
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
			if out, ok2 := inlineCSSURL(s, baseDir); ok2 {
				sec[k] = out
			}
		}
	}
	return m
}

func inlineCSSURL(s, baseDir string) (string, bool) {
	val := strings.TrimSpace(s)
	if !strings.HasPrefix(val, "url(") || !strings.HasSuffix(val, ")") {
		return s, false
	}
	inner := strings.Trim(strings.TrimSpace(val[4:len(val)-1]), `'"`)
	if inner == "" || strings.Contains(inner, "://") ||
		strings.HasPrefix(inner, "data:") || strings.HasPrefix(inner, "/") {
		return s, false
	}
	for _, cand := range themeImageCandidates(inner, baseDir) {
		data, err := os.ReadFile(cand)
		if err != nil || len(data) == 0 || len(data) > 8<<20 {
			continue
		}
		return "url(data:" + themeImageMime(cand) + ";base64," + base64.StdEncoding.EncodeToString(data) + ")", true
	}
	return s, false
}

// themeImageCandidates 依次尝试：按主题目录解析、按老写法里的 themes/colors/ 之后的部分、
// 同目录同名文件、按 exe 根目录解析。
func themeImageCandidates(inner, baseDir string) []string {
	p := filepath.FromSlash(inner)
	out := []string{filepath.Join(baseDir, p)}
	marker := filepath.Join("themes", "colors") + string(filepath.Separator)
	if i := strings.LastIndex(p, marker); i >= 0 {
		out = append(out, filepath.Join(baseDir, p[i+len(marker):]))
	}
	out = append(out, filepath.Join(baseDir, filepath.Base(p)))
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), p))
	}
	return out
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

func ListExternalIconThemes() []ExternalThemeInfo {
	dir := filepath.Join(themeDir(), "icons")
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []ExternalThemeInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		themePath := filepath.Join(dir, e.Name(), "icons.json")
		data, err := os.ReadFile(themePath)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		info := ExternalThemeInfo{
			ID:   e.Name(),
			Name: m["_name"],
		}
		if v, ok := m["_modes"].([]any); ok {
			for _, mv := range v {
				if s, ok2 := mv.(string); ok2 {
					info.Modes = append(info.Modes, s)
				}
			}
		}
		if v, ok := m["_fallback"].(string); ok {
			info.Fallback = v
		}
		result = append(result, info)
	}
	return result
}

func LoadExternalIconTheme(id string) map[string]any {
	dir := filepath.Join(themeDir(), "icons", id)
	if dir == "" {
		return nil
	}
	// Read icons.json for metadata
	meta, err := os.ReadFile(filepath.Join(dir, "icons.json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(meta, &m); err != nil {
		return nil
	}
	// Read SVG files alongside icons.json
	icons := make(map[string]any)
	svgEntries, err := os.ReadDir(dir)
	if err == nil {
		for _, se := range svgEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".svg") {
				continue
			}
			svgData, err := os.ReadFile(filepath.Join(dir, se.Name()))
			if err != nil {
				continue
			}
			key := strings.TrimSuffix(se.Name(), ".svg")
			icons[key] = string(svgData)
		}
	}
	m["icons"] = icons
	return m
}

func listExternalThemes(dir string) []ExternalThemeInfo {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var result []ExternalThemeInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if v, ok := m["_id"].(string); ok && v != "" {
			id = v
		}
		info := ExternalThemeInfo{
			ID:   id,
			Name: m["_name"],
		}
		if v, ok := m["_modes"].([]any); ok {
			for _, mv := range v {
				if s, ok2 := mv.(string); ok2 {
					info.Modes = append(info.Modes, s)
				}
			}
		}
		if v, ok := m["_fallback"].(string); ok {
			info.Fallback = v
		}
		result = append(result, info)
	}
	return result
}

func loadExternalThemeJSON(path string) map[string]any {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}
