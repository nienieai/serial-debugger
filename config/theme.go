package config

import (
	"embed"
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

func LoadTheme(id string) map[string]any {
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
	entries, err := themesFS.ReadDir("themes")
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			ids = append(ids, name[:len(name)-5])
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
	return loadExternalThemeJSON(filepath.Join(themeDir(), "colors", id+".json"))
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
