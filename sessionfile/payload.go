package sessionfile

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// 槽 payload 的编码：TLV 表，key(uint16 LE) | len(uint16 LE) | value[len]。
//
// 为什么不是 JSON：JSON 一改结构就得升版本号，读的一方还要按版本分支。
// 这里 key 的类型由下面的常量表约定，条目不存类型字节；要加设置就分配一个
// 没人用过的 key，老读者遇到不认识的 key 直接跳过即可 —— **加字段永远不需要
// 动 SlotPayloadVer**。取值方式真要变时换一个新 key，旧 key 的读法留着。
//
// 号段：
//
//	0x0001        身份与格式
//	0x0010-0x001F 录制元数据（uuid / created / label / 端口 / 录制模式）
//	0x0100-0x01FF 显示设置
//	0x0200-0x02FF 绘图预留
//	0x8000-0xFFFF 私有/实验
const (
	KeyPayloadVer uint16 = 0x0001

	KeyUUID    uint16 = 0x0010
	KeyCreated uint16 = 0x0011
	KeyLabel   uint16 = 0x0012
	KeyMode    uint16 = 0x0013
	KeyPort    uint16 = 0x0014
	KeyBaud    uint16 = 0x0015
	KeyPortB   uint16 = 0x0016

	KeyDisplayEncoding        uint16 = 0x0100
	KeyDisplayHexCase         uint16 = 0x0101
	KeyDisplayHexPrefix       uint16 = 0x0102
	KeyDisplayHexSep          uint16 = 0x0103
	KeyDisplayCrVisible       uint16 = 0x0104
	KeyDisplayHexEscapeMode   uint16 = 0x0105
	KeyDisplayHexEscapeFormat uint16 = 0x0106
	KeyDisplayCopyHexEscapes  uint16 = 0x0107
	KeyDisplayTabSize         uint16 = 0x0108
	KeyDisplayEolSequence     uint16 = 0x0109
)

var (
	ErrPayloadTruncated = errors.New("stlog: slot payload truncated")
	ErrPayloadNoVer     = errors.New("stlog: slot payload missing version")
)

// displayKeyNames 是显示设置的 TLV key 与 settings.json 里 display 段键名的
// 对应关系。顺序即写出顺序（key 升序）。
var displayKeyNames = []struct {
	key  uint16
	name string
}{
	{KeyDisplayEncoding, "encoding"},
	{KeyDisplayHexCase, "hexCase"},
	{KeyDisplayHexPrefix, "hexPrefix"},
	{KeyDisplayHexSep, "hexSep"},
	{KeyDisplayCrVisible, "crVisible"},
	{KeyDisplayHexEscapeMode, "hexEscapeMode"},
	{KeyDisplayHexEscapeFormat, "hexEscapeFormat"},
	{KeyDisplayCopyHexEscapes, "copyHexEscapes"},
	{KeyDisplayTabSize, "tabSize"},
	{KeyDisplayEolSequence, "eolSequence"},
}

var displayNameByKey = func() map[uint16]string {
	m := make(map[uint16]string, len(displayKeyNames))
	for _, d := range displayKeyNames {
		m[d.key] = d.name
	}
	return m
}()

// EncodeSettings 把 Settings 编成槽 payload。
//
// 空字符串和 0 值不写（读回来就是零值，不必占字节）。payload 里始终带
// PayloadVer，读的一侧靠它确认这是一份 TLV 而不是旧的 JSON。
func EncodeSettings(s Settings) []byte {
	v := s.V
	if v == 0 {
		v = SlotPayloadVer
	}
	out := make([]byte, 0, 160)
	out = appendU16(out, KeyPayloadVer, uint16(v))
	out = appendStr(out, KeyUUID, s.UUID)
	out = appendStr(out, KeyCreated, s.Created)
	out = appendStr(out, KeyLabel, s.Label)
	out = appendStr(out, KeyMode, s.Mode)
	out = appendStr(out, KeyPort, s.Port)
	out = appendU32(out, KeyBaud, uint32(s.Baud))
	out = appendStr(out, KeyPortB, s.PortB)
	for _, d := range displayKeyNames {
		out = appendStr(out, d.key, s.Display[d.name])
	}
	return out
}

// DecodeSettings 解析槽 payload。
//
// 不认识的 key 跳过（这正是 TLV 存在的意义），重复 key 取第一个。只有结构性
// 错误（条目越界、缺 PayloadVer）才报错：值长度对不上的条目按「认不出来」处理，
// 不让一条坏数据把整个文件判死。
func DecodeSettings(payload []byte) (Settings, error) {
	var s Settings
	seen := make(map[uint16]bool, len(displayKeyNames)+8)
	for off := 0; off < len(payload); {
		if off+4 > len(payload) {
			return Settings{}, ErrPayloadTruncated
		}
		key := binary.LittleEndian.Uint16(payload[off:])
		n := int(binary.LittleEndian.Uint16(payload[off+2:]))
		off += 4
		if off+n > len(payload) {
			return Settings{}, ErrPayloadTruncated
		}
		val := payload[off : off+n]
		off += n

		if seen[key] {
			continue
		}
		switch key {
		case KeyPayloadVer:
			if n == 2 {
				s.V = int(binary.LittleEndian.Uint16(val))
				seen[key] = true
			}
		case KeyUUID:
			s.UUID, seen[key] = string(val), true
		case KeyCreated:
			s.Created, seen[key] = string(val), true
		case KeyLabel:
			s.Label, seen[key] = string(val), true
		case KeyMode:
			s.Mode, seen[key] = string(val), true
		case KeyPort:
			s.Port, seen[key] = string(val), true
		case KeyPortB:
			s.PortB, seen[key] = string(val), true
		case KeyBaud:
			if n == 4 {
				s.Baud = int(binary.LittleEndian.Uint32(val))
				seen[key] = true
			}
		default:
			name, ok := displayNameByKey[key]
			if !ok {
				continue // 还没实现的新字段：跳过
			}
			if s.Display == nil {
				s.Display = make(map[string]string, len(displayKeyNames))
			}
			s.Display[name] = string(val)
			seen[key] = true
		}
	}
	if s.V == 0 {
		return Settings{}, ErrPayloadNoVer
	}
	return s, nil
}

// DisplayStrings 把 settings.json 里 display 段的各种 JSON 值压成字符串表，
// 供包头槽的 TLV 使用（槽里每个 value 都是一段 UTF-8）。
func DisplayStrings(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case nil:
			continue
		case string:
			out[k] = t
		case bool:
			out[k] = "false"
			if t {
				out[k] = "true"
			}
		default:
			out[k] = fmt.Sprint(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendEntry(dst []byte, key uint16, val []byte) []byte {
	var h [4]byte
	binary.LittleEndian.PutUint16(h[0:], key)
	binary.LittleEndian.PutUint16(h[2:], uint16(len(val)))
	dst = append(dst, h[:]...)
	return append(dst, val...)
}

func appendStr(dst []byte, key uint16, s string) []byte {
	if s == "" {
		return dst
	}
	// 单条不可能撑满一个槽；这里只防 uint16 长度回绕。
	if len(s) > MaxSlotPayload {
		s = s[:MaxSlotPayload]
	}
	return appendEntry(dst, key, []byte(s))
}

func appendU16(dst []byte, key uint16, v uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[0:], v)
	return appendEntry(dst, key, b[:])
}

func appendU32(dst []byte, key uint16, v uint32) []byte {
	if v == 0 {
		return dst
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[0:], v)
	return appendEntry(dst, key, b[:])
}
