package config

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed i18n/*.json
var i18nFS embed.FS

var (
	i18nData = make(map[string]map[string]string) // lang -> key -> text
	i18nMu   sync.RWMutex
)

func init() {
	entries, err := i18nFS.ReadDir("i18n")
	if err != nil {
		panic("i18n: cannot read embedded translations: " + err.Error())
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// filename: "zh.json" -> lang: "zh"
		name := e.Name()
		if len(name) < 6 || name[len(name)-5:] != ".json" {
			continue
		}
		lang := name[:len(name)-5]
		data, err := i18nFS.ReadFile("i18n/" + name)
		if err != nil {
			continue
		}
		m := make(map[string]string)
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		i18nData[lang] = m
	}
}

// T translates key for the given language, with optional args for fmt.Sprintf.
func T(lang, key string, args ...any) string {
	i18nMu.RLock()
	m, ok := i18nData[lang]
	i18nMu.RUnlock()
	if !ok {
		m, _ = i18nData["zh"]
		if m == nil {
			if len(args) > 0 {
				return fmt.Sprintf(key, args...)
			}
			return key
		}
	}
	s, ok := m[key]
	if !ok {
		s = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// HasLang returns true if the language code is supported.
func HasLang(lang string) bool {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	_, ok := i18nData[lang]
	return ok
}

// GetI18nMap returns all key-value pairs for a language (used by GUI via Wails).
func GetI18nMap(lang string) map[string]string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	m, ok := i18nData[lang]
	if !ok {
		m = i18nData["zh"]
	}
	if m == nil {
		return map[string]string{}
	}
	// Return a copy to avoid concurrent access issues
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// FormatSysMsg translates a system message stored in key|arg1|arg2|... format.
// Args that look like i18n keys (contain '.') are translated recursively via T().
// Returns the raw string unchanged if it doesn't start with "sys.".
func FormatSysMsg(lang, raw string) string {
	if raw == "" || len(raw) < 4 || raw[:4] != "sys." {
		return raw
	}
	parts := strings.Split(raw, "|")
	key := parts[0]
	args := parts[1:]

	tmpl := T(lang, key)
	if tmpl == key {
		return raw // key not found, return raw
	}
	for _, arg := range args {
		// Translate arg if it's an i18n key (has dots)
		val := arg
		if strings.Contains(arg, ".") {
			if t := T(lang, arg); t != arg {
				val = t
			}
		}
		if idx := strings.Index(tmpl, "%s"); idx >= 0 {
			tmpl = tmpl[:idx] + val + tmpl[idx+2:]
		} else if idx := strings.Index(tmpl, "%d"); idx >= 0 {
			tmpl = tmpl[:idx] + val + tmpl[idx+2:]
		}
	}
	return tmpl
}
