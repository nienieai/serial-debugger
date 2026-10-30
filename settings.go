package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/nienieai/serial-debugger/ini"
)

// AppSettings holds all user-facing GUI settings persisted to disk.
//
// 设置按「谁用谁存」拆成目录：<exe>/settings/{gui,cli,daemon}.ini。
// 这个结构体只对应 gui.ini —— GUI 独占写，CLI 与守护进程各有自己的文件
// （见 daemon/settings.go 与 cmd/serial-cli/settings.go），MCP 不落盘。
type AppSettings struct {
	DisplayMode       string  `json:"displayMode" ini:"display_mode"`
	SendRatio         float64 `json:"sendRatio" ini:"send_ratio"`
	QuickPanelRatio   float64 `json:"quickPanelRatio" ini:"quick_panel_ratio"`
	Encoding          string  `json:"encoding" ini:"encoding"`
	HexCase           string  `json:"hexCase" ini:"hex_case"`
	HexPrefix         bool    `json:"hexPrefix" ini:"hex_prefix"`
	HexSep            string  `json:"hexSep" ini:"hex_sep"`
	CrVisible         bool    `json:"crVisible" ini:"cr_visible"`
	HexEscapeMode     string  `json:"hexEscapeMode" ini:"hex_escape_mode"`
	HexEscapeFormat   string  `json:"hexEscapeFormat" ini:"hex_escape_format"`
	CopyHexEscapes    bool    `json:"copyHexEscapes" ini:"copy_hex_escapes"`
	DisplayFontFamily string  `json:"displayFontFamily" ini:"display_font_family"`
	DisplayFontSize   int     `json:"displayFontSize" ini:"display_font_size"`
	TabSize           int     `json:"tabSize" ini:"tab_size"`
	EolSequence       string  `json:"eolSequence" ini:"eol_sequence"`
	Theme             string  `json:"theme" ini:"theme"`
	ColorThemeID      string  `json:"colorThemeId" ini:"color_theme_id"`
	IconThemeID       string  `json:"iconThemeId" ini:"icon_theme_id"`
	Language          string  `json:"language" ini:"language"`
	AutoCreateSession bool    `json:"autoCreateSession" ini:"auto_create_session"`
	// AutoSaveHistory 是「默认自动保存历史」：新标签 / 新进程是否把数据写进
	// history/*.log。默认关闭（零值就是 false），只有用户显式打开才落盘。
	AutoSaveHistory bool   `json:"autoSaveHistory" ini:"auto_save_history"`
	DisplayColors   string `json:"displayColors" ini:"display_colors"`
	// StlogShowYear 控制 .stlog 文件页「行时间戳」是否带年份（MM-DD → YYYY-MM-DD）。
	// 顶栏的时间范围永远带年，这个开关只影响逐行显示：带年一行宽 10 个字符，
	// 紧凑优先的人可以关掉。默认开。
	StlogShowYear bool `json:"stlogShowYear" ini:"stlog_show_year"`

	Serial   SerialDefaults   `json:"serial" ini:"serial"`
	AutoSend AutoSendDefaults `json:"autoSend" ini:"autosend"`
	Append   AppendDefaults   `json:"append" ini:"append"`
}

type SerialDefaults struct {
	Baud        int    `json:"baud" ini:"baud"`
	DataBits    int    `json:"dataBits" ini:"data_bits"`
	StopBits    string `json:"stopBits" ini:"stop_bits"`
	Parity      string `json:"parity" ini:"parity"`
	FlowControl string `json:"flowControl" ini:"flow_control"`
}

type AutoSendDefaults struct {
	IntervalMs int `json:"intervalMs" ini:"interval_ms"`
}

type AppendDefaults struct {
	Suffix string `json:"suffix" ini:"suffix"`
}

var iniSectionOrder = []string{"display", "serial", "autosend", "append"}

func defaultSettings() *AppSettings {
	return &AppSettings{
		DisplayMode:       "text",
		SendRatio:         0.3,
		QuickPanelRatio:   0,
		Encoding:          "utf-8",
		HexCase:           "upper",
		HexPrefix:         true,
		HexSep:            "space",
		CrVisible:         true,
		HexEscapeMode:     "show",
		HexEscapeFormat:   "slash",
		CopyHexEscapes:    true,
		DisplayFontFamily: "Consolas",
		DisplayFontSize:   14,
		TabSize:           4,
		EolSequence:       "lf",
		Theme:             "auto",
		ColorThemeID:      "theme-default",
		IconThemeID:       "",
		Language:          "zh",
		AutoCreateSession: true,
		StlogShowYear:     true,
		Serial: SerialDefaults{
			Baud:        115200,
			DataBits:    8,
			StopBits:    "1",
			Parity:      "none",
			FlowControl: "none",
		},
		AutoSend: AutoSendDefaults{
			IntervalMs: 1000,
		},
		Append: AppendDefaults{
			Suffix: "",
		},
	}
}

// settingsDir 返回与 exe 同级的设置目录（<exe>/settings）。
func settingsDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return ini.Dir(filepath.Dir(exe)), nil
}

// settingsPath 返回 GUI 自己的设置文件 <exe>/settings/gui.ini。
//
// 一个文件只有一个写者：GUI 只写 gui.ini，CLI 只写 cli.ini（第一版只读），
// 守护进程只写 daemon.ini。此前三者共用一份 settings.ini，而保存是整文件
// 截断重写，一旦两方都写就会互相抹掉对方的字段。
func settingsPath() (string, error) {
	dir, err := settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gui.ini"), nil
}

// LoadAppSettings reads persisted settings; returns defaults when the file
// does not exist or cannot be parsed.
func (a *App) LoadAppSettings() *AppSettings {
	cfg := defaultSettings()
	p, err := settingsPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	if err := unmarshalINI(data, cfg); err != nil {
		return defaultSettings()
	}
	return cfg
}

// SaveAppSettings writes settings to <exe>/settings/gui.ini.
func (a *App) SaveAppSettings(s *AppSettings) error {
	if s == nil {
		return nil
	}
	p, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return marshalINI(f, s)
}

// SettingsDir 返回设置目录，供前端「打开设置文件夹」与关于页展示。
func (a *App) SettingsDir() string {
	dir, err := settingsDir()
	if err != nil {
		return ""
	}
	return dir
}

// ---- INI marshal / unmarshal ----

func marshalINI(w *os.File, cfg *AppSettings) error {
	bw := bufio.NewWriter(w)
	first := true
	for _, sec := range iniSectionOrder {
		if !first {
			bw.WriteString("\n")
		}
		first = false
		fmt.Fprintf(bw, "[%s]\n", sec)
		v := reflect.ValueOf(cfg).Elem()
		var secVal reflect.Value
		switch sec {
		case "display":
			secVal = v
		case "serial":
			secVal = v.FieldByName("Serial")
		case "autosend":
			secVal = v.FieldByName("AutoSend")
		case "append":
			secVal = v.FieldByName("Append")
		}
		t := secVal.Type()
		for i := 0; i < secVal.NumField(); i++ {
			tag := t.Field(i).Tag.Get("ini")
			if tag == "" {
				continue
			}
			val := secVal.Field(i)
			var s string
			switch val.Kind() {
			case reflect.String:
				s = val.String()
			case reflect.Int:
				s = strconv.Itoa(int(val.Int()))
			case reflect.Float64:
				s = strconv.FormatFloat(val.Float(), 'f', -1, 64)
			case reflect.Bool:
				s = strconv.FormatBool(val.Bool())
			}
			fmt.Fprintf(bw, "%s = %s\n", tag, s)
		}
	}
	return bw.Flush()
}

func unmarshalINI(data []byte, cfg *AppSettings) error {
	lookup := buildINILookup(cfg)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		// 记事本/PowerShell 写出的文件可能带 UTF-8 BOM，首行会多一个 \ufeff。
		line := strings.TrimPrefix(scanner.Text(), "\ufeff")
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '[' || line[0] == '#' || line[0] == ';' {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if ptr, ok := lookup[key]; ok {
			if err := setField(ptr, val); err != nil {
				fmt.Fprintf(os.Stderr, "gui.ini: invalid value for '%s': %v\n", key, err)
			}
		}
	}
	return scanner.Err()
}

func buildINILookup(cfg *AppSettings) map[string]interface{} {
	m := make(map[string]interface{})
	registerFields(m, reflect.ValueOf(cfg).Elem())
	registerFields(m, reflect.ValueOf(&cfg.Serial).Elem())
	registerFields(m, reflect.ValueOf(&cfg.AutoSend).Elem())
	registerFields(m, reflect.ValueOf(&cfg.Append).Elem())
	return m
}

func registerFields(m map[string]interface{}, v reflect.Value) {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		tag := t.Field(i).Tag.Get("ini")
		if tag != "" {
			m[tag] = v.Field(i).Addr().Interface()
		}
	}
}

func setField(ptr interface{}, val string) error {
	switch p := ptr.(type) {
	case *string:
		*p = val
	case *int:
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		*p = n
	case *float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		*p = f
	case *bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		*p = b
	}
	return nil
}
