package config

import "embed"

//go:embed icons/system/*.svg
var iconsFS embed.FS

var builtinIcons map[string]string

func init() {
	entries, err := iconsFS.ReadDir("icons/system")
	if err != nil {
		return
	}
	builtinIcons = make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) <= 4 || name[len(name)-4:] != ".svg" {
			continue
		}
		data, err := iconsFS.ReadFile("icons/system/" + name)
		if err != nil {
			continue
		}
		key := name[:len(name)-4] // Strip .svg
		builtinIcons[key] = string(data)
	}
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

// GetBuiltinIcons returns all built-in icons as a map.
func GetBuiltinIcons() map[string]string {
	return builtinIcons
}
