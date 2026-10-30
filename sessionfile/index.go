package sessionfile

import (
	"encoding/binary"
	"os"
	"path/filepath"
)

// Index 是会话的包级索引，纯缓存：删掉、损坏、与磁盘不符时一律丢弃重建，
// 任何情况下都不因为索引有问题而拒绝打开会话。
//
// 只做「包级」摘要（每个包多大、多少帧、时间范围）。页级索引留到需要时再加，
// 它只在会话真的长大到几十个包时才影响打开速度。
type Index struct {
	Entries []IndexEntry
}

// IndexEntry 对应一个包的摘要。
type IndexEntry struct {
	Seq     int
	Bytes   int64
	Frames  uint64
	Records uint64
	FirstTs uint64
	LastTs  uint64
	Mtime   int64 // 包文件修改时间（UnixNano），用于判断缓存是否过期
	Sealed  bool
}

const (
	indexMagic  uint32 = 0x58494C53 // "SLIX"
	indexVer    uint32 = 1
	indexHdrLen        = 16
	indexEntLen        = 64
)

// Lookup 按包序号取摘要。
func (idx *Index) Lookup(seq int) (IndexEntry, bool) {
	if idx == nil {
		return IndexEntry{}, false
	}
	for _, e := range idx.Entries {
		if e.Seq == seq {
			return e, true
		}
	}
	return IndexEntry{}, false
}

// SaveIndex 原子写入 index.bin。
func SaveIndex(dir string, idx *Index) error {
	buf := make([]byte, indexHdrLen+len(idx.Entries)*indexEntLen)
	binary.LittleEndian.PutUint32(buf[0:], indexMagic)
	binary.LittleEndian.PutUint32(buf[4:], indexVer)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(idx.Entries)))
	for i, e := range idx.Entries {
		b := buf[indexHdrLen+i*indexEntLen:]
		binary.LittleEndian.PutUint32(b[0:], uint32(e.Seq))
		binary.LittleEndian.PutUint64(b[8:], uint64(e.Bytes))
		binary.LittleEndian.PutUint64(b[16:], e.Frames)
		binary.LittleEndian.PutUint64(b[24:], e.Records)
		binary.LittleEndian.PutUint64(b[32:], e.FirstTs)
		binary.LittleEndian.PutUint64(b[40:], e.LastTs)
		binary.LittleEndian.PutUint64(b[48:], uint64(e.Mtime))
		if e.Sealed {
			b[56] = 1
		}
	}
	return writeFileAtomic(filepath.Join(dir, IndexName), buf)
}

// LoadIndex 读取 index.bin 并按 packges 校验。第二个返回值为 false 表示
// 缓存不可用（不存在 / 损坏 / 与磁盘不符），调用方应重建后覆盖。
//
// 校验项刻意做得严：任一条对不上就整份丢弃。索引只是加速，宁可重扫也不要
// 让一份半旧的缓存误导上层。
func LoadIndex(dir string, pkgs []PkgRef) (*Index, bool) {
	path := filepath.Join(dir, IndexName)
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if fi.Size() < indexHdrLen {
		return nil, false
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if binary.LittleEndian.Uint32(buf[0:]) != indexMagic {
		return nil, false
	}
	if binary.LittleEndian.Uint32(buf[4:]) != indexVer {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(buf[8:]))
	if n != len(pkgs) || len(buf) < indexHdrLen+n*indexEntLen {
		return nil, false
	}

	idx := &Index{Entries: make([]IndexEntry, 0, n)}
	for i := 0; i < n; i++ {
		b := buf[indexHdrLen+i*indexEntLen:]
		e := IndexEntry{
			Seq:     int(binary.LittleEndian.Uint32(b[0:])),
			Bytes:   int64(binary.LittleEndian.Uint64(b[8:])),
			Frames:  binary.LittleEndian.Uint64(b[16:]),
			Records: binary.LittleEndian.Uint64(b[24:]),
			FirstTs: binary.LittleEndian.Uint64(b[32:]),
			LastTs:  binary.LittleEndian.Uint64(b[40:]),
			Mtime:   int64(binary.LittleEndian.Uint64(b[48:])),
			Sealed:  b[56] == 1,
		}
		// 与 manifest 的登记项逐个对齐
		if e.Seq != pkgs[i].Seq {
			return nil, false
		}
		pfi, err := os.Stat(filepath.Join(dir, pkgs[i].File))
		if err != nil {
			return nil, false
		}
		if pfi.Size() != e.Bytes || pfi.ModTime().UnixNano() != e.Mtime {
			return nil, false
		}
		idx.Entries = append(idx.Entries, e)
	}
	return idx, true
}
