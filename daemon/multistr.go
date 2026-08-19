package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"serial-tool-v3/ringbuf"
)

// MultistrEntry represents one entry in the multi-string send panel.
type MultistrEntry struct {
	Enabled bool   `json:"enabled"`
	Hex     bool   `json:"hex"`
	Content string `json:"content"`
	Delay   int    `json:"delay"`
	Note    string `json:"note"`
}

// ── binary format (ring buffer handles [2B len] framing) ──
//
// [1B version = 0x01]
// [1B flags]                bit0=enabled, bit1=hex
// [2B LE delay_ms]          per-entry delay (1–60000)
// [1B note_len]             note length in bytes (0–255)
// [note_len B note_utf8]
// [remaining bytes content]

const multistrVersion = 0x01
const multistrMinLen = 5 // version + flags + delay(2) + note_len

// EncodeEntry serializes a single entry to binary.
// Note: the ring buffer adds its own length prefix; do NOT prepend length here.
func EncodeEntry(e MultistrEntry) []byte {
	noteBytes := []byte(e.Note)
	contentBytes := []byte(e.Content)

	totalLen := 1 + 1 + 2 + 1 + len(noteBytes) + len(contentBytes) // version + flags + delay + note_len + note + content
	buf := make([]byte, totalLen)

	buf[0] = multistrVersion

	var flags byte
	if e.Enabled {
		flags |= 0x01
	}
	if e.Hex {
		flags |= 0x02
	}
	buf[1] = flags

	delay := e.Delay
	if delay < 1 {
		delay = 1000
	}
	if delay > 60000 {
		delay = 60000
	}
	binary.LittleEndian.PutUint16(buf[2:4], uint16(delay))

	buf[4] = byte(len(noteBytes))
	copy(buf[5:5+len(noteBytes)], noteBytes)
	copy(buf[5+len(noteBytes):], contentBytes)

	return buf
}

// DecodeEntry deserializes a binary packet into a MultistrEntry.
func DecodeEntry(data []byte) (MultistrEntry, error) {
	if len(data) < multistrMinLen {
		return MultistrEntry{}, fmt.Errorf("packet too short: %d < %d", len(data), multistrMinLen)
	}
	if data[0] != multistrVersion {
		return MultistrEntry{}, fmt.Errorf("unknown version: %d", data[0])
	}

	flags := data[1]
	enabled := flags&0x01 != 0
	isHex := flags&0x02 != 0

	delay := int(binary.LittleEndian.Uint16(data[2:4]))
	noteLen := int(data[4])

	if 5+noteLen > len(data) {
		return MultistrEntry{}, fmt.Errorf("note length %d exceeds packet length %d", noteLen, len(data))
	}

	note := string(data[5 : 5+noteLen])
	content := string(data[5+noteLen:])

	return MultistrEntry{
		Enabled: enabled,
		Hex:     isHex,
		Content: content,
		Delay:   delay,
		Note:    note,
	}, nil
}

// DecodeEntryContentOnly extracts just the content bytes from a binary entry
// (for single mode, which ignores metadata).
// Returns the raw data unchanged if it doesn't look like a valid multistr entry.
func DecodeEntryContentOnly(data []byte) []byte {
	if len(data) < multistrMinLen || data[0] != multistrVersion {
		return data
	}
	// Validate flags: only bits 0 (enabled) and 1 (hex) are valid
	flags := data[1]
	if flags > 0x03 {
		return data
	}
	// Validate delay: must be in 5–60000 ms range
	delay := int(binary.LittleEndian.Uint16(data[2:4]))
	if delay < 5 || delay > 60000 {
		return data
	}
	noteLen := int(data[4])
	if 5+noteLen > len(data) {
		return data
	}
	return data[5+noteLen:]
}

// ── sendq batch operations ──

// ReadAllEntries reads all entries from the send queue ring buffer.
// Returns parsed entries in order (oldest first).
func ReadAllEntries(rb *ringbuf.RingBuffer) []MultistrEntry {
	if rb == nil {
		return nil
	}
	packets := rb.DrainAll()
	entries := make([]MultistrEntry, 0, len(packets))
	for _, pkt := range packets {
		e, err := DecodeEntry(pkt)
		if err != nil {
			// Treat unparseable entries as text content with defaults
			e = MultistrEntry{
				Enabled: true,
				Hex:     false,
				Content: string(pkt),
				Delay:   1000,
				Note:    "",
			}
		}
		entries = append(entries, e)
	}
	return entries
}

// WriteAllEntries writes all entries to the send queue ring buffer.
// Clears existing content first.
func WriteAllEntries(rb *ringbuf.RingBuffer, entries []MultistrEntry) error {
	if rb == nil {
		return fmt.Errorf("ring buffer is nil")
	}
	rb.Reset()
	for _, e := range entries {
		pkt := EncodeEntry(e)
		if !rb.Write(pkt) {
			return fmt.Errorf("send queue full after %d entries", len(entries))
		}
	}
	return nil
}

// WriteOneEntry writes a single entry to the send queue.
func WriteOneEntry(rb *ringbuf.RingBuffer, e MultistrEntry) error {
	if rb == nil {
		return fmt.Errorf("ring buffer is nil")
	}
	pkt := EncodeEntry(e)
	if !rb.Write(pkt) {
		return fmt.Errorf("send queue full")
	}
	return nil
}

// ── persistence ──

func multistrDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "multistr"
	}
	return filepath.Join(filepath.Dir(exe), "multistr")
}

func multistrFilePath(processId string) string {
	return filepath.Join(multistrDir(), processId+".json")
}

// SaveEntriesToFile persists entries to a JSON file.
func SaveEntriesToFile(processId string, entries []MultistrEntry) error {
	dir := multistrDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create multistr dir: %w", err)
	}

	path := multistrFilePath(processId)
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encode entries: %w", err)
	}
	f.Close()

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// LoadEntriesFromFile loads entries from a JSON file.
func LoadEntriesFromFile(processId string) ([]MultistrEntry, error) {
	path := multistrFilePath(processId)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []MultistrEntry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode entries: %w", err)
	}
	return entries, nil
}

// ── helpers ──

// EntryToRaw converts a MultistrEntry to raw bytes suitable for serial port writing.
func EntryToRaw(e MultistrEntry) ([]byte, error) {
	if e.Hex {
		// Remove spaces from hex string
		cleaned := strings.ReplaceAll(e.Content, " ", "")
		cleaned = strings.ReplaceAll(cleaned, "\r", "")
		cleaned = strings.ReplaceAll(cleaned, "\n", "")
		raw, err := hex.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("invalid hex content: %w", err)
		}
		return raw, nil
	}
	return []byte(e.Content), nil
}
