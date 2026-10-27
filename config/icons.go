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

// 图标包：resources/icons/<pack>/icons.json + *.svg，随二进制一起嵌入。
// 磁盘上的 <exe>/resources/icons/<pack>/ 覆盖同名包；加包只需新建一个目录。
//
//go:embed resources/icons/*/*.svg
//go:embed resources/icons/*/icons.json
var iconsFS embed.FS

const iconsEmbedRoot = "resources/icons"

// 不再在 init() 里缓存图标表：每次调用都按磁盘（resources/icons/<pack>/）优先重新读，
// 这样新增/修改 .svg 后只要前端刷新一次就生效，不用重启 exe。
// 一个包就几十个小文件，读一次的开销可以忽略。

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

// loadIconPack 读一个图标包，返回 key -> svg。
// 按资源层优先级找（内置 resources-system → 外置 resources）：哪一层里有这个包的
// .svg 就用哪一层；都没有再退回嵌入的那份。
func loadIconPack(pack string) map[string]string {
	for _, base := range resourceDirs() {
		if out := loadIconDir(filepath.Join(base, "icons", pack)); len(out) > 0 {
			return out
		}
	}
	dir := iconsEmbedRoot + "/" + pack
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

// isBuiltinIconPack 判断图标包属于内置层（resources-system 目录，或编译进 exe 的那份）。
func isBuiltinIconPack(pack string) bool {
	if d := builtinResourceDir(); d != "" && fileExists(filepath.Join(d, "icons", pack, "icons.json")) {
		return true
	}
	_, err := iconsFS.ReadFile(iconsEmbedRoot + "/" + pack + "/icons.json")
	return err == nil
}

// ListBuiltinIconPacks 列出图标包（各资源层 icons/ 下的子目录，含 icons.json 的才算）。
// 显示顺序：内置层强制在前，组内按 id 字母序。
func ListBuiltinIconPacks() []string {
	seen := map[string]bool{}
	var packs []string
	for _, base := range resourceDirs() {
		dir := filepath.Join(base, "icons")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if !fileExists(filepath.Join(dir, e.Name(), "icons.json")) {
				continue
			}
			if !seen[e.Name()] {
				seen[e.Name()] = true
				packs = append(packs, e.Name())
			}
		}
	}
	entries, err := fs.ReadDir(iconsFS, iconsEmbedRoot)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if _, err := iconsFS.ReadFile(iconsEmbedRoot + "/" + e.Name() + "/icons.json"); err != nil {
				continue
			}
			seen[e.Name()] = true
			packs = append(packs, e.Name())
		}
	}
	sort.SliceStable(packs, func(i, j int) bool {
		bi, bj := isBuiltinIconPack(packs[i]), isBuiltinIconPack(packs[j])
		if bi != bj {
			return bi
		}
		return packs[i] < packs[j]
	})
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
	if p := resourcePath("icons", id, "icons.json"); fileExists(p) {
		if data, err := os.ReadFile(p); err == nil {
			return data, nil
		}
	}
	return iconsFS.ReadFile(iconsEmbedRoot + "/" + id + "/icons.json")
}

// GetBuiltinIconTheme 返回某个内置图标包（结构同 LoadExternalIconTheme）：
// icons.json 的元数据 + { "icons": { key: svg } }。
func GetBuiltinIconTheme(id string) map[string]any {
	if id == "" {
		id = "system"
	}
	var data []byte
	var err error
	if p := resourcePath("icons", id, "icons.json"); fileExists(p) {
		data, err = os.ReadFile(p)
	}
	if len(data) == 0 || err != nil {
		data, err = iconsFS.ReadFile(iconsEmbedRoot + "/" + id + "/icons.json")
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
	return loadIconPack("system")[key]
}

// ListBuiltinIconKeys returns all built-in icon keys.
func ListBuiltinIconKeys() []string {
	icons := loadIconPack("system")
	keys := make([]string, 0, len(icons))
	for k := range icons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetBuiltinIcons returns all built-in icons as a map (system pack).
// 每次调用都读盘，前端刷新即拿到最新的图标集合。
func GetBuiltinIcons() map[string]string {
	return loadIconPack("system")
}
