package sessionfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SettingsDoc 是会话目录里的 settings.json。
//
// 所有「会变」的业务设置都放这里，manifest 只管连接。将来加绘图设置
// （选中的通道、量程、坐标、区域、标注）也往 Plot 里塞，不需要动 manifest。
//
// 与包头槽里的 Settings 的关系：
//   - 槽里那份是**冗余自描述**，保证单个 pkg 拎出来也能读（v2 §3）
//   - settings.json 是**权威**，改设置不影响已封包的字节
type SettingsDoc struct {
	V     int          `json:"v"`
	Label string       `json:"label,omitempty"`
	Notes string       `json:"notes,omitempty"`
	Mode  string       `json:"mode,omitempty"` // 空串等价 ModeSingle
	Ports SessionPorts `json:"ports"`
	// Log 记与之同时录制的那份 .log（history 里的文件名），便于成对查找。
	Log string `json:"log,omitempty"`
	// Plot 预留给绘图：选中通道、量程、坐标轴、区域、标注等。
	// 用 map 而不是具体结构，先保证读写不丢未知字段。
	Plot map[string]any `json:"plot,omitempty"`
	// Display 是这条会话的显示设置：编码、Hex 大小写/前缀/分隔、控制字符、
	// 转义字节怎么显示。只影响「怎么看」，不影响已封包的字节。
	//
	// 同样是 map：这些键以后还会加，读写时不该因为旧版本不认识就把它们丢掉。
	Display map[string]any `json:"display,omitempty"`
	// Save 记的是「关掉的那一刻这个工程还在录」。推送回守护进程时据此决定要不要
	// 接着录 —— 续写的是同一个目录（见 daemon 的 OpenSession），不是另起一个会话。
	Save bool `json:"save,omitempty"`
}

// SessionPorts 是 settings.json 里的端口段。转发录制时 A/B 都有值。
type SessionPorts struct {
	A    string `json:"a,omitempty"`
	B    string `json:"b,omitempty"`
	Baud int    `json:"baud,omitempty"`
}

// SettingsDocVer 是 settings.json 的结构版本。
const SettingsDocVer = 1

// NewSettingsDoc 从包头槽里的 Settings 生成一份 settings.json。
func NewSettingsDoc(set Settings) *SettingsDoc {
	doc := &SettingsDoc{
		V:     SettingsDocVer,
		Label: set.Label,
		Mode:  set.Mode,
		Ports: SessionPorts{A: set.Port, B: set.PortB, Baud: set.Baud},
	}
	if doc.Mode == "" {
		doc.Mode = ModeSingle
	}
	if len(set.Display) > 0 {
		doc.Display = make(map[string]any, len(set.Display))
		for k, v := range set.Display {
			doc.Display[k] = v
		}
	}
	return doc
}

// IsForward 报告这份设置是不是转发录制。
func (d *SettingsDoc) IsForward() bool { return d != nil && d.Mode == ModeForward }

// ToSettings 把 settings.json 还原成包头槽用的 Settings。
func (d *SettingsDoc) ToSettings() Settings {
	if d == nil {
		return Settings{}
	}
	mode := d.Mode
	if mode == "" {
		mode = ModeSingle
	}
	return Settings{
		V:       SlotPayloadVer,
		Label:   d.Label,
		Port:    d.Ports.A,
		Baud:    d.Ports.Baud,
		Mode:    mode,
		PortB:   d.Ports.B,
		Display: DisplayStrings(d.Display),
	}
}

// LoadSettingsDoc 读取 settings.json。
func LoadSettingsDoc(dir string) (*SettingsDoc, error) {
	b, err := os.ReadFile(filepath.Join(dir, SettingsName))
	if err != nil {
		return nil, err
	}
	var d SettingsDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d.V == 0 {
		d.V = SettingsDocVer
	}
	return &d, nil
}

// SaveSettingsDoc 原子写入 settings.json。
func SaveSettingsDoc(dir string, d *SettingsDoc) error {
	if d.V == 0 {
		d.V = SettingsDocVer
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(filepath.Join(dir, SettingsName), b)
}
