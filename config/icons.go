package config

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

// 内置图标包：config/icons/<pack>/icons.json + *.svg，随二进制一起嵌入。
// 和 config/i18n/*.json 一样，每个包是独立文件，加包只需新建一个目录。
//
//go:embed icons/*/*.svg
//go:embed icons/*/icons.json
var iconsFS embed.FS

var builtinIcons map[string]string

func init() {
	builtinIcons = loadIconPack("system")
}

// loadIconPack 读一个内置图标包，返回 key -> svg
func loadIconPack(pack string) map[string]string {
	dir := "icons/" + pack
	entries, err := fs.ReadDir(iconsFS, dir)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= 4 || !strings.HasSuffix(name, ".svg") {
			continue
		}
		data, err := iconsFS.ReadFile(dir + "/" + name)
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(name, ".svg")] = string(data)
	}
	return out
}

// ListBuiltinIconPacks 列出内置图标包（icons/ 下的子目录，含 icons.json 的才算）。
func ListBuiltinIconPacks() []string {
	entries, err := fs.ReadDir(iconsFS, "icons")
	if err != nil {
		return nil
	}
	var packs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := iconsFS.ReadFile("icons/" + e.Name() + "/icons.json"); err != nil {
			continue
		}
		packs = append(packs, e.Name())
	}
	sort.Strings(packs)
	return packs
}

// ListBuiltinIconThemes 内置图标包的主题信息，字段和外置的 ExternalThemeInfo 对齐，
// 前端可以走同一条渲染路径。
func ListBuiltinIconThemes() []ExternalThemeInfo {
	packs := ListBuiltinIconPacks()
	result := make([]ExternalThemeInfo, 0, len(packs))
	for _, id := range packs {
		info := ExternalThemeInfo{ID: id}
		if data, err := iconsFS.ReadFile("icons/" + id + "/icons.json"); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				info.Name = m["_name"]
				if v, ok := m["_fallback"].(string); ok {
					info.Fallback = v
				}
				if arr, ok := m["_modes"].([]any); ok {
					for _, it := range arr {
						if s, ok := it.(string); ok {
							info.Modes = append(info.Modes, s)
						}
					}
				}
			}
		}
		result = append(result, info)
	}
	return result
}

// GetBuiltinIconTheme 返回某个内置图标包（结构同 LoadExternalIconTheme）：
// icons.json 的元数据 + { "icons": { key: svg } }。
func GetBuiltinIconTheme(id string) map[string]any {
	if id == "" {
		id = "system"
	}
	data, err := iconsFS.ReadFile("icons/" + id + "/icons.json")
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	icons := loadIconPack(id)
	wrapped := make(map[string]any, len(icons)+len(m))
	for k, v := range m {
		wrapped[k] = v
	}
	anyIcons := make(map[string]any, len(icons))
	for k, v := range icons {
		anyIcons[k] = v
	}
	wrapped["icons"] = anyIcons
	return wrapped
}

// GetBuiltinIcon returns the built-in SVG icon for the given key.
// Key is the filename without .svg (e.g. "plus", "close", "settings").
func GetBuiltinIcon(key string) string {
	return builtinIcons[key]
}

// ListBuiltinIconKeys returns all built-in icon keys.
func ListBuiltinIconKeys() []string {
	keys := make([]string, 0, len(builtinIcons))
	for k := range builtinIcons {
		keys = append(keys, k)
	}
	return keys
}

// GetBuiltinIcons returns all built-in icons as a map (system pack).
func GetBuiltinIcons() map[string]string {
	return builtinIcons
}
