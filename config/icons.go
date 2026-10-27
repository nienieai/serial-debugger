package config

import (
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
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

// loadIconDir 从一个真实目录读 key -> svg（目录不存在返回 nil）
func loadIconDir(dir string) map[string]string {
	entries, err := os.ReadDir(dir)
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
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(name, ".svg")] = string(data)
	}
	return out
}

// loadIconPack 读一个内置图标包，返回 key -> svg。
// 磁盘 builtin/icons/<pack>/ 优先，方便查看和替换；没有再退回嵌入的那份。
func loadIconPack(pack string) map[string]string {
	if bd := builtinDir(); bd != "" {
		if out := loadIconDir(filepath.Join(bd, "icons", pack)); len(out) > 0 {
			return out
		}
	}
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
	seen := map[string]bool{}
	var packs []string
	// 磁盘 builtin/icons/ 里的包优先
	if bd := builtinDir(); bd != "" {
		if entries, err := os.ReadDir(filepath.Join(bd, "icons")); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if _, err := os.Stat(filepath.Join(bd, "icons", e.Name(), "icons.json")); err != nil {
					continue
				}
				if !seen[e.Name()] {
					seen[e.Name()] = true
					packs = append(packs, e.Name())
				}
			}
		}
	}
	entries, err := fs.ReadDir(iconsFS, "icons")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if _, err := iconsFS.ReadFile("icons/" + e.Name() + "/icons.json"); err != nil {
				continue
			}
			seen[e.Name()] = true
			packs = append(packs, e.Name())
		}
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
		if data, err := _readBuiltinIconMeta(id); err == nil {
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

func _readBuiltinIconMeta(id string) ([]byte, error) {
	if bd := builtinDir(); bd != "" {
		if data, err := os.ReadFile(filepath.Join(bd, "icons", id, "icons.json")); err == nil {
			return data, nil
		}
	}
	return iconsFS.ReadFile("icons/" + id + "/icons.json")
}

// GetBuiltinIconTheme 返回某个内置图标包（结构同 LoadExternalIconTheme）：
// icons.json 的元数据 + { "icons": { key: svg } }。
func GetBuiltinIconTheme(id string) map[string]any {
	if id == "" {
		id = "system"
	}
	var data []byte
	var err error
	if bd := builtinDir(); bd != "" {
		data, err = os.ReadFile(filepath.Join(bd, "icons", id, "icons.json"))
	}
	if len(data) == 0 || err != nil {
		data, err = iconsFS.ReadFile("icons/" + id + "/icons.json")
		if err != nil {
			return nil
		}
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
