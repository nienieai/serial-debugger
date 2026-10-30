package sessionfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// v2 会话目录（一个会话 = 一个文件夹）里固定的几个文件名。
const (
	ManifestName = "manifest.json"
	SettingsName = "settings.json"
	IndexName    = "index.bin"

	ManifestVer = 1
)

// Manifest 是会话目录的入口文件。
//
// 「仅做连接」：这里只放会话身份和指向目录内其它文件的引用，任何业务设置
// （端口、模式、标签、将来的绘图配置）都在 settings.json 里。这样改设置
// 永远不会碰入口文件，入口文件也不会因为新增一类设置而变大。
//
// 帧数 / 记录数 / 时间范围这些统计刻意不放在这里：它们是缓存，放 index.bin，
// 坏了可以重建，而 manifest 只描述「这个会话由哪些文件组成」。
type Manifest struct {
	V         int           `json:"v"`
	SessionID string        `json:"sessionId"` // 完整 uuid；目录名只取前 8 位
	Created   string        `json:"created"`   // RFC3339
	Files     ManifestFiles `json:"files"`
}

// ManifestFiles 指目录内其余文件：包是列表，设置与索引各一个。
type ManifestFiles struct {
	Settings string   `json:"settings"`
	Index    string   `json:"index"`
	Packages []PkgRef `json:"packages"`
}

// PkgRef 是一个包在 manifest 里的登记项：只记「怎么找到它」和「多大」。
// 没登记进 manifest 的包由扫描认领（见 orphan 处理）。
type PkgRef struct {
	Seq    int    `json:"seq"` // 从 1 起连续，跳号即缺包
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	Sealed bool   `json:"sealed"`
}

// PkgFileName 返回第 seq 个包的文件名。seq 从 1 开始。
func PkgFileName(seq int) string { return fmt.Sprintf("pkg-%04d.stlog", seq) }

// SessionDirName 按 v2 规则生成会话目录名：yyyyMMdd_HHmmss_<sessionId 前 8 位>。
//
// 时间到秒：目录会长期保留，同一分钟建两个必须分得开。
// 后缀不是进程号：进程号随 daemon 重启重排，跨天恢复或换机器都会撞。
func SessionDirName(t time.Time, sessionID string) string {
	id := sessionID
	if len(id) > 8 {
		id = id[:8]
	}
	return t.Local().Format("20060102_150405") + "_" + id
}

// LoadManifest 读取目录里的 manifest.json。
func LoadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("stlog: manifest.json 解析失败: %w", err)
	}
	if m.V != ManifestVer {
		return nil, fmt.Errorf("stlog: manifest 版本 %d 不支持（期望 %d）", m.V, ManifestVer)
	}
	if m.SessionID == "" {
		return nil, fmt.Errorf("stlog: manifest 缺 sessionId")
	}
	return &m, nil
}

// SaveManifest 原子写入 manifest.json：先写 .tmp 再 rename，崩在中间只会留下
// 一个可以忽略的 tmp 文件，旧 manifest 仍然完整。
func SaveManifest(dir string, m *Manifest) error {
	if m.V == 0 {
		m.V = ManifestVer
	}
	if m.Files.Settings == "" {
		m.Files.Settings = SettingsName
	}
	if m.Files.Index == "" {
		m.Files.Index = IndexName
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeFileAtomic(filepath.Join(dir, ManifestName), b)
}

// writeFileAtomic 写临时文件 → fsync → rename。
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
