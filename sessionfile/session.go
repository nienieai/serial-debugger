package sessionfile

import (
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 包上限。默认值来自 docs/format/stlog-v2.md §4.1。
const (
	DefaultPkgMaxBytes int64 = 64 << 20 // 64 MiB
	MinPkgMaxBytes     int64 = 8 << 20
	MaxPkgMaxBytes     int64 = 1 << 30
)

// Session 是一次录制 = 一个目录。
//
// 目录里：manifest.json（仅做连接）、settings.json（业务设置）、index.bin（缓存）、
// pkg-NNNN.stlog（数据包，每个都是完整合法的 v1.4 文件）。
//
// 写路径上唯一的活是「切包」：帧绝不被切开跨包，宁可提前封包留空。
type Session struct {
	mu       sync.Mutex
	dir      string
	manifest *Manifest
	doc      *SettingsDoc
	pkgMax   int64
	w        *Writer // 当前包；封包后为 nil
	seq      int     // 当前包序号

	// 当前包的累计统计。Writer 里的 Stats 是页级的，换页就重置，
	// 所以包级的帧数/时间范围要在这里自己记。
	frames  int
	firstTs uint64
	lastTs  uint64

	entries []IndexEntry // 已封包 + 当前的摘要，用于写 index.bin
	closed  bool
}

// NewSessionID 生成 32 位十六进制 uuid（v4）。
func NewSessionID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		// 极端情况下退回时间派生，只要不撞到同一个值就够用
		n := uint64(time.Now().UnixNano())
		for i := 0; i < 8; i++ {
			b[i] = byte(n >> (8 * uint(i)))
			b[8+i] = byte(n >> (8 * uint(7-i)))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

// CreateSession 在 dir（会话目录本身）建一个新会话。
// 已存在同名目录时会复用并追加，调用方若要独占请先确认目录为空。
func CreateSession(dir string, set Settings) (*Session, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	id := set.UUID
	if id == "" {
		id = NewSessionID()
	}
	created := set.Created
	if created == "" {
		created = time.Now().Format(time.RFC3339)
	}

	// 包头的槽也要带身份，保证单个 pkg 拎出去仍然自描述（v2 §3）
	set.V = SlotPayloadVer
	set.UUID = id
	set.Created = created

	m := &Manifest{
		V:         ManifestVer,
		SessionID: id,
		Created:   created,
		Files:     ManifestFiles{Settings: SettingsName, Index: IndexName},
	}
	doc := NewSettingsDoc(set)
	if err := SaveSettingsDoc(dir, doc); err != nil {
		return nil, err
	}

	s := &Session{dir: dir, manifest: m, doc: doc, pkgMax: DefaultPkgMaxBytes}
	if err := s.openPackage(set); err != nil {
		return nil, err
	}
	if err := SaveManifest(dir, m); err != nil {
		return nil, err
	}
	return s, nil
}

// CreateSessionIn 在 parent 下按 v2 命名规则自动建一个会话目录。
func CreateSessionIn(parent string, set Settings) (*Session, error) {
	id := set.UUID
	if id == "" {
		id = NewSessionID()
	}
	dir := filepath.Join(parent, SessionDirName(time.Now(), id))
	// 撞号（概率约 4e-9）：加 -2/-3，不改时间戳
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			break
		}
		dir = filepath.Join(parent, SessionDirName(time.Now(), id)+"-"+strconv.Itoa(n))
	}
	set.UUID = id
	return CreateSession(dir, set)
}

// Dir 返回会话目录。
func (s *Session) Dir() string { return s.dir }

// SessionID 返回完整 uuid。
func (s *Session) SessionID() string { return s.manifest.SessionID }

// SettingsDoc 返回当前设置（可改，Close 前调 SaveSettings 落盘）。
func (s *Session) SettingsDoc() *SettingsDoc { return s.doc }

// SetPkgMaxBytes 调整单包上限（会夹到 [8 MiB, 1 GiB]）。只影响后续的切包判断。
func (s *Session) SetPkgMaxBytes(n int64) {
	if n < MinPkgMaxBytes {
		n = MinPkgMaxBytes
	}
	if n > MaxPkgMaxBytes {
		n = MaxPkgMaxBytes
	}
	s.pkgMax = n
}

// PkgSeq 返回当前包序号（从 1 开始）。
func (s *Session) PkgSeq() int { return s.seq }

// openPackage 建下一个包并登记到 manifest。
func (s *Session) openPackage(set Settings) error {
	seq := s.seq + 1
	name := PkgFileName(seq)
	w, err := CreateWriter(filepath.Join(s.dir, name), set)
	if err != nil {
		return err
	}
	s.w = w
	s.seq = seq
	s.frames = 0
	s.firstTs, s.lastTs = 0, 0
	s.manifest.Files.Packages = append(s.manifest.Files.Packages, PkgRef{Seq: seq, File: name})
	return nil
}

// Append 写一帧。写之前先做准入判断：装不下就封包、开新包。
//
// 代价按 docs/format/stlog-v2.md §4.2 的 entryCost 估算：
//
//	12 + len(payload) + (inline origin ? 1 + len(originStr) : 0)
//
// 这是保守估计（不含跨页时的页头开销），宁可早一点切包。
func (s *Session) Append(ts uint64, dir uint8, origin uint8, originStr string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("stlog: session 已关闭")
	}
	if s.w == nil {
		return errors.New("stlog: session 没有可写的包")
	}

	cost := int64(RecordHeaderLen + len(payload))
	if origin == OriginInline {
		cost += int64(1 + len(originStr))
	}

	// 只切「非空的包」。空包不切，才能让超大的单帧独占一包（软上限）。
	if s.w.AllocatedBytes()+cost > s.pkgMax && s.w.AllocatedBytes() > HeaderSize {
		if err := s.sealPackage(); err != nil {
			return err
		}
		if err := s.openPackage(s.doc.ToSettings()); err != nil {
			return err
		}
	}

	if err := s.w.Append(ts, dir, origin, originStr, payload); err != nil {
		return err
	}
	if s.frames == 0 {
		s.firstTs = ts
	}
	s.lastTs = ts
	s.frames++
	return nil
}

// sealPackage 封当前包：flush + close，更新 manifest 登记项与索引摘要。
func (s *Session) sealPackage() error {
	if s.w == nil {
		return nil
	}
	w := s.w
	s.w = nil
	if err := w.Close(); err != nil {
		return err
	}
	// 以磁盘上的真实大小 / mtime 为准：AllocatedBytes 是逻辑占用（尾页没写过
	// 时也会多算一页），而索引要靠这两个值和文件对齐才能判断缓存是否过期。
	bytes := w.AllocatedBytes()
	var mtime int64
	if fi, err := os.Stat(filepath.Join(s.dir, PkgFileName(s.seq))); err == nil {
		bytes = fi.Size()
		mtime = fi.ModTime().UnixNano()
	}
	if ref := s.lastRef(); ref != nil {
		ref.Bytes = bytes
		ref.Sealed = true
	}
	s.entries = append(s.entries, IndexEntry{
		Seq:     s.seq,
		Bytes:   bytes,
		Frames:  uint64(s.frames),
		Records: uint64(s.frames), // 记录数在写入时不可得（跨页会拆成多条），留待扫描补
		FirstTs: s.firstTs,
		LastTs:  s.lastTs,
		Mtime:   mtime,
		Sealed:  true,
	})
	return nil
}

func (s *Session) lastRef() *PkgRef {
	if n := len(s.manifest.Files.Packages); n > 0 {
		return &s.manifest.Files.Packages[n-1]
	}
	return nil
}

// Flush 把当前包的内存页写盘。定时刷盘用，语义与 Writer.Flush 一致。
func (s *Session) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return nil
	}
	return s.w.Flush()
}

// SaveSettings 把内存里的 settings.json 写盘（改过 label/notes/plot 之后调用）。
func (s *Session) SaveSettings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SaveSettingsDoc(s.dir, s.doc)
}

// Close 封包 → 写 manifest → 写索引。可重复调用。
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.sealPackage(); err != nil {
		return err
	}
	if err := SaveManifest(s.dir, s.manifest); err != nil {
		return err
	}
	return SaveIndex(s.dir, &Index{Entries: s.entries})
}

// PackageInfo 是读取时对单个包的摘要。
type PackageInfo struct {
	Seq     int
	File    string
	Bytes   int64
	Frames  int
	Records int
	FirstTs uint64
	LastTs  uint64
	Sealed  bool
	Err     error
}

// ReadSession 读会话：path 是目录则按 v2 读，是文件则按 v1.4 单文件读。
//
// 单文件入口保持不变，history 里已有的 .stlog 不会因为升级而打不开。
func ReadSession(path string) (*FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		info, err := ReadFile(path)
		if err != nil {
			return nil, err
		}
		info.IsSession = false
		return info, nil
	}
	return readSessionDir(path)
}

// PackageRefs 返回会话目录里参与本次会话的包：manifest 登记优先，
// manifest 缺失或为空时按文件名认领。
func PackageRefs(dir string) []PkgRef {
	if m, err := LoadManifest(dir); err == nil && len(m.Files.Packages) > 0 {
		return m.Files.Packages
	}
	return scanPackages(dir)
}

// scanPackages 在 manifest 缺失时按文件名认领包。
func scanPackages(dir string) []PkgRef {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []PkgRef
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "pkg-") || !strings.HasSuffix(name, ".stlog") {
			continue
		}
		seq, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "pkg-"), ".stlog"))
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, PkgRef{Seq: seq, File: name, Bytes: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func readSessionDir(dir string) (*FileInfo, error) {
	info := &FileInfo{Path: dir, IsSession: true}

	var pkgs []PkgRef
	if m, err := LoadManifest(dir); err == nil {
		info.SessionID = m.SessionID
		info.Created = m.Created
		pkgs = m.Files.Packages
	}
	if len(pkgs) == 0 {
		// manifest 缺失/损坏：以磁盘上的包为准（label/notes 丢失，其余不丢）
		pkgs = scanPackages(dir)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("stlog: %s 里没有找到任何 pkg-*.stlog", dir)
	}

	// settings.json 权威；没有就退回第一个包的槽
	if doc, err := LoadSettingsDoc(dir); err == nil {
		set := doc.ToSettings()
		// settings.json 不重复存身份，UUID/Created 从 manifest 补上，
		// 保证上层拿到的 Settings 和 v1.4 单文件里的槽一样完整。
		set.UUID = info.SessionID
		set.Created = info.Created
		info.Settings = &set
		info.SettingsDoc = doc
	}

	for _, ref := range pkgs {
		pinfo := PackageInfo{Seq: ref.Seq, File: ref.File, Sealed: ref.Sealed}
		sub, err := ReadFile(filepath.Join(dir, ref.File))
		if err != nil {
			pinfo.Err = err
			info.Packages = append(info.Packages, pinfo)
			if info.StopErr == nil {
				info.StopErr = fmt.Errorf("包 %s: %w", ref.File, err)
			}
			continue
		}
		pinfo.Bytes = sub.Size
		pinfo.Frames = len(sub.Frames)
		pinfo.Records = sub.Records
		if len(sub.Pages) > 0 {
			pinfo.FirstTs = sub.Pages[0].FirstTs
			pinfo.LastTs = sub.Pages[len(sub.Pages)-1].LastTs
		}
		info.Packages = append(info.Packages, pinfo)
		mergePackageInfo(info, sub, ref.Seq, &pinfo)
	}
	return info, nil
}

// mergePackageInfo 把一个包的解码结果并进会话视图。
func mergePackageInfo(dst, sub *FileInfo, seq int, pinfo *PackageInfo) {
	if dst.Settings == nil {
		dst.Settings = sub.Settings
	}
	for i := range sub.Pages {
		p := sub.Pages[i]
		p.Package = seq
		dst.Pages = append(dst.Pages, p)
	}
	dst.Frames = append(dst.Frames, sub.Frames...)
	dst.Records += sub.Records
	dst.Size += sub.Size
	dst.RxTotal += sub.RxTotal
	dst.TxTotal += sub.TxTotal
	dst.P1Total += sub.P1Total
	dst.P2Total += sub.P2Total
	dst.Recovered = append(dst.Recovered, sub.Recovered...)
	dst.Truncated = dst.Truncated || sub.Truncated
	dst.BrokenChain = dst.BrokenChain || sub.BrokenChain
	if sub.StoppedAt != 0 && dst.StoppedAt == 0 {
		dst.StoppedAt = sub.StoppedAt
	}
	if sub.StopErr != nil && dst.StopErr == nil {
		dst.StopErr = sub.StopErr
	}
}
