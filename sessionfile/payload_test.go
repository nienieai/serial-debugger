package sessionfile

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sampleSettings() Settings {
	return Settings{
		V:       SlotPayloadVer,
		UUID:    "0123456789abcdef0123456789abcdef",
		Created: "2026-10-31T16:00:00+08:00",
		Label:   "COM9",
		Port:    "COM9",
		Baud:    921600,
		Mode:    ModeForward,
		PortB:   "COM14",
		Display: map[string]string{
			"encoding":       "gbk",
			"hexCase":        "lower",
			"hexPrefix":      "false",
			"hexSep":         "none",
			"crVisible":      "false",
			"tabSize":        "8",
			"eolSequence":    "crlf",
			"copyHexEscapes": "false",
		},
	}
}

// TestSettingsTLVRoundTrip 钉住编解码：所有字段原样回来，包括只在 v3 才有的 display。
func TestSettingsTLVRoundTrip(t *testing.T) {
	in := sampleSettings()
	payload := EncodeSettings(in)
	if payload[0] != 0x01 || payload[1] != 0x00 {
		t.Fatalf("payload 应以 key 0x0001 开头，实际 % x", payload[:4])
	}

	out, err := DecodeSettings(payload)
	if err != nil {
		t.Fatalf("DecodeSettings: %v", err)
	}
	if out.V != in.V || out.UUID != in.UUID || out.Created != in.Created ||
		out.Label != in.Label || out.Port != in.Port || out.Baud != in.Baud ||
		out.Mode != in.Mode || out.PortB != in.PortB {
		t.Fatalf("身份字段不一致:\n got %+v\nwant %+v", out, in)
	}
	for k, want := range in.Display {
		if got := out.Display[k]; got != want {
			t.Fatalf("display[%s] = %q, want %q", k, got, want)
		}
	}
	if len(out.Display) != len(in.Display) {
		t.Fatalf("display 条目数 = %d, want %d", len(out.Display), len(in.Display))
	}
}

// TestSettingsTLVIgnoresUnknownKey 是这套格式的核心承诺：以后加字段只分配新 key，
// 老代码读新文件不能因此失败，也不能把别的字段读坏。
func TestSettingsTLVIgnoresUnknownKey(t *testing.T) {
	payload := EncodeSettings(sampleSettings())
	// 模拟未来的 0x0017 = 某个字符串字段
	extra := make([]byte, 4+5)
	binary.LittleEndian.PutUint16(extra[0:], 0x0017)
	binary.LittleEndian.PutUint16(extra[2:], 5)
	copy(extra[4:], "hello")
	payload = append(payload, extra...)

	out, err := DecodeSettings(payload)
	if err != nil {
		t.Fatalf("未知 key 不该让解析失败: %v", err)
	}
	if out.Label != "COM9" || out.PortB != "COM14" {
		t.Fatalf("已知字段被读坏: %+v", out)
	}
	if out.Display["encoding"] != "gbk" {
		t.Fatalf("display 被读坏: %+v", out.Display)
	}
}

// TestSettingsTLVDuplicateKeyFirstWins 定下重复 key 的语义（取第一个），
// 免得以后追加写的人猜。
func TestSettingsTLVDuplicateKeyFirstWins(t *testing.T) {
	var payload []byte
	payload = appendStr(payload, KeyLabel, "first")
	payload = appendStr(payload, KeyLabel, "second")
	payload = appendU16(payload, KeyPayloadVer, SlotPayloadVer)

	out, err := DecodeSettings(payload)
	if err != nil {
		t.Fatalf("DecodeSettings: %v", err)
	}
	if out.Label != "first" {
		t.Fatalf("重复 key 应取第一个, got %q", out.Label)
	}
}

// TestSettingsTLVTruncated 确认坏结构会被拒，而不是读出一半字段。
func TestSettingsTLVTruncated(t *testing.T) {
	payload := EncodeSettings(sampleSettings())
	if _, err := DecodeSettings(payload[:len(payload)-3]); !errors.Is(err, ErrPayloadTruncated) {
		t.Fatalf("截断应报 ErrPayloadTruncated, got %v", err)
	}
	if _, err := DecodeSettings(nil); !errors.Is(err, ErrPayloadNoVer) {
		t.Fatalf("空 payload 应报 ErrPayloadNoVer, got %v", err)
	}
	if _, err := DecodeSettings([]byte{0x01, 0x00, 0x00}); !errors.Is(err, ErrPayloadTruncated) {
		t.Fatalf("半截条目头应报 ErrPayloadTruncated, got %v", err)
	}
}

// TestSettingsTLVOmitsEmpty 确认空值不占字节：一个空 Settings 只剩版本条目。
func TestSettingsTLVOmitsEmpty(t *testing.T) {
	payload := EncodeSettings(Settings{})
	if len(payload) != 6 {
		t.Fatalf("空 Settings 的 payload 应为 6 字节, got %d (% x)", len(payload), payload)
	}
	if SlotPayloadVer != 3 {
		t.Fatalf("SlotPayloadVer = %d, want 3", SlotPayloadVer)
	}
	out, err := DecodeSettings(payload)
	if err != nil {
		t.Fatalf("DecodeSettings: %v", err)
	}
	if out.V != SlotPayloadVer {
		t.Fatalf("V = %d, want %d", out.V, SlotPayloadVer)
	}
}

// TestStlogHeaderKeepsDisplay 是端到端的那一条：写进包头槽的显示设置，
// 重新打开单个 pkg 时能读回来 —— 这正是「拎一个文件出来也能按当初的样子看」。
func TestStlogHeaderKeepsDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disp.stlog")
	set := Settings{
		Label: "COM9", Port: "COM9", Baud: 115200,
		Display: map[string]string{"encoding": "gbk", "hexCase": "lower", "tabSize": "8"},
	}
	w, err := CreateWriter(path, set)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1000, DirRx, OriginNone, "", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if info.Settings == nil {
		t.Fatal("包头槽没读出 settings")
	}
	if info.Settings.Display["encoding"] != "gbk" {
		t.Fatalf("display.encoding = %q, want gbk", info.Settings.Display["encoding"])
	}
	if info.Settings.Display["tabSize"] != "8" {
		t.Fatalf("display.tabSize = %q, want 8", info.Settings.Display["tabSize"])
	}
	if info.Settings.Label != "COM9" {
		t.Fatalf("label = %q, want COM9", info.Settings.Label)
	}
}

// TestReadSettingsLegacyJSON 确认 v2 的老文件（payload 是 JSON）还能打开。
func TestReadSettingsLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.stlog")
	w, err := CreateWriter(path, Settings{Label: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(1000, DirRx, OriginNone, "", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	legacy := []byte(`{"v":2,"uuid":"abc","label":"old","port":"COM1","baud":9600}`)
	sa, err := EncodeSlot(&Slot{Seq: 10, Ts: 1, Payload: legacy})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(sa, SlotAOffset); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	info, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if info.Settings == nil {
		t.Fatal("老格式的槽没读出来")
	}
	if info.Settings.Label != "old" || info.Settings.Port != "COM1" || info.Settings.Baud != 9600 {
		t.Fatalf("老格式读错: %+v", info.Settings)
	}
}

// TestDisplayStrings 确认 settings.json 里的各种 JSON 值都能压成字符串。
func TestDisplayStrings(t *testing.T) {
	got := DisplayStrings(map[string]any{
		"encoding":  "utf-8",
		"hexPrefix": true,
		"tabSize":   float64(8),
		"crVisible": nil,
	})
	want := map[string]string{"encoding": "utf-8", "hexPrefix": "true", "tabSize": "8"}
	if len(got) != len(want) {
		t.Fatalf("DisplayStrings = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("DisplayStrings[%s] = %q, want %q", k, got[k], v)
		}
	}
	if DisplayStrings(nil) != nil {
		t.Fatal("nil 应返回 nil")
	}
}
