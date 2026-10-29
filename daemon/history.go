package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nienieai/serial-debugger/ringbuf"
)

// 历史落盘的**默认值**：新进程是否把数据写进 history/*.log。
//
// 默认关闭：只有用户显式开启才生成历史文件（以前默认开，跑一次就多一个
// 日志文件）。单个进程还可以被 history.enable{processId} 覆盖，见
// Process.SetSaveHistory。启动时由 settings/daemon.ini 的 [history] auto_save
// 覆盖（GUI 连上后还会用自身的「默认自动保存」推一次 history.enable）。
var (
	historyDefaultSave   = false
	historyDefaultSaveMu sync.RWMutex
)

// defaultSaveHistory 返回新进程的默认落盘开关。
func defaultSaveHistory() bool {
	historyDefaultSaveMu.RLock()
	defer historyDefaultSaveMu.RUnlock()
	return historyDefaultSave
}

// setDefaultSaveHistory 修改默认值；已经显式设置过（SetSaveHistory）的进程不受影响。
func setDefaultSaveHistory(v bool) {
	historyDefaultSaveMu.Lock()
	historyDefaultSave = v
	historyDefaultSaveMu.Unlock()
}

// historyDirOverride 仅测试用：指向临时目录，避免测试往二进制旁写 history/。
var historyDirOverride string

func historyDir() string {
	if historyDirOverride != "" {
		return historyDirOverride
	}
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
//
// 包长由头部 11 字节算出；长度字段最高位为 1 时后面还跟着「来源长度(1B) +
// 来源字符串」。原样返回（含来源段），这样写入环形缓冲的字节与文件里完全一致。
func readHistoryRaw(r *bufio.Reader) ([]byte, error) {
	var header [11]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	bodyLen, hasOrigin := historyPacketLen(header[:])
	pkt := make([]byte, bodyLen)
	copy(pkt[:11], header[:])
	if _, err := io.ReadFull(r, pkt[11:]); err != nil {
		return nil, fmt.Errorf("truncated entry: %w", err)
	}
	if hasOrigin {
		originLen, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("truncated origin length: %w", err)
		}
		pkt = append(pkt, originLen)
		if originLen > 0 {
			origin := make([]byte, originLen)
			if _, err := io.ReadFull(r, origin); err != nil {
				return nil, fmt.Errorf("truncated origin: %w", err)
			}
			pkt = append(pkt, origin...)
		}
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
		if strings.Contains(hexUpper, kwUpper) || containsDecodedHex(entry.Hex, kwUpper) {
			results = append(results, entry)
		}
	}

	return results, pos, nil
}

// containsDecodedHex reports whether keyword occurs in the plain text carried
// by a data entry.
//
// Data entries store their payload hex-encoded, while system messages store
// literal text in the same field. Matching only entry.Hex therefore meant a
// user-typed plain keyword ("OK") could never match a data entry (4F4B), even
// though the same keyword did match system entries. Decoding first makes plain
// text searches work for both, while the raw comparison above still supports
// searching by hex.
func containsDecodedHex(hexText, kwUpper string) bool {
	raw, err := hex.DecodeString(hexText)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToUpper(string(raw)), kwUpper)
}

func readHistoryEntry(r *bufio.Reader) (HistoryEntry, int64, error) {
	pkt, err := readHistoryRaw(r)
	if err != nil {
		return HistoryEntry{}, 0, err
	}
	entry, ok := decodeHistoryPacket(pkt)
	if !ok {
		return HistoryEntry{}, 0, fmt.Errorf("invalid history packet")
	}
	return entry, int64(len(pkt)), nil
}
