package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/ringbuf"
)

var (
	historyEnabled   = true
	historyEnabledMu sync.RWMutex
)

func isHistoryEnabled() bool {
	historyEnabledMu.RLock()
	defer historyEnabledMu.RUnlock()
	return historyEnabled
}

func setHistoryEnabled(v bool) {
	historyEnabledMu.Lock()
	historyEnabled = v
	historyEnabledMu.Unlock()
}

func historyDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "history"
	}
	return filepath.Join(filepath.Dir(exe), "history")
}

// newHistoryFile creates a new timestamped history file for append.
func newHistoryFile(dir, procID string) (*os.File, string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}
	name := time.Now().Format("200601021504") + "_" + procID + ".log"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

// attachHistoryFile opens an existing history file for append.
func attachHistoryFile(dir, filename string) (*os.File, string, error) {
	path := filepath.Join(dir, filepath.Base(filename))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

// loadHistoryIntoRing reads all entries from a history file and writes them
// into the ring buffer so the frontend can display historical data.
func loadHistoryIntoRing(rb *ringbuf.RingBuffer, filePath string) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	count := 0
	for {
		pkt, err := readHistoryRaw(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		if !rb.Write(pkt) {
			return count, fmt.Errorf("ring buffer full after %d entries", count)
		}
		count++
	}
	return count, nil
}

// readHistoryRaw reads one raw packet (the exact bytes as stored on disk).
func readHistoryRaw(r *bufio.Reader) ([]byte, error) {
	var header [11]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	hexLen := binary.LittleEndian.Uint16(header[9:11])
	pkt := make([]byte, 11+hexLen)
	copy(pkt[:11], header[:])
	if _, err := io.ReadFull(r, pkt[11:]); err != nil {
		return nil, fmt.Errorf("truncated entry: %w", err)
	}
	return pkt, nil
}

func appendHistoryPacket(f *os.File, pkt []byte) error {
	if f == nil {
		return nil
	}
	_, err := f.Write(pkt)
	return err
}

func closeHistoryFile(f *os.File) {
	if f != nil {
		f.Close()
	}
}

// ── file listing ──

type HistoryFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

func listHistoryFiles(dir string) ([]HistoryFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []HistoryFileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, HistoryFileInfo{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return result, nil
}

// ── search ──

func searchHistoryFile(path string, keyword string, limit int, offset int64) ([]HistoryEntry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, 0, err
		}
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	var results []HistoryEntry
	var pos int64 = offset

	for len(results) < limit {
		entry, n, err := readHistoryEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		pos += n

		hexUpper := strings.ToUpper(entry.Hex)
		kwUpper := strings.ToUpper(keyword)
		if strings.Contains(hexUpper, kwUpper) {
			results = append(results, entry)
		}
	}

	return results, pos, nil
}

func readHistoryEntry(r *bufio.Reader) (HistoryEntry, int64, error) {
	var header [11]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return HistoryEntry{}, 0, err
	}

	ts := int64(binary.LittleEndian.Uint64(header[0:8]))
	dir := header[8]
	hexLen := binary.LittleEndian.Uint16(header[9:11])

	hexBuf := make([]byte, hexLen)
	if _, err := io.ReadFull(r, hexBuf); err != nil {
		return HistoryEntry{}, 0, fmt.Errorf("truncated entry: %w", err)
	}

	consumed := int64(11 + hexLen)
	return HistoryEntry{
		Timestamp: time.UnixMilli(ts).Format("15:04:05.000"),
		Hex:       string(hexBuf),
		Direction: byteToDir(dir),
	}, consumed, nil
}
