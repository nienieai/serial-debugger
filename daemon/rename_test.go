package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nienieai/serial-debugger/sessionfile"
)

// 线程名：默认按端口/会话号现算，改名后固定，清空后回到自动名。
func TestProcessNameDefaultAndRename(t *testing.T) {
	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll) // 不清理的话共享内存会留给后面的用例撞名

	// 没有端口：自动名是「会话 #<id>」
	idle, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil {
		t.Fatalf("建空闲进程失败: %v", err)
	}
	wantAuto := "会话 #" + idle.id
	if got := idle.Name(); got != wantAuto {
		t.Fatalf("空闲进程自动名 = %q，期望 %q", got, wantAuto)
	}
	if idle.HasCustomName() {
		t.Fatal("没改过名却报告有自定义名")
	}

	// 声明端口后自动名立刻跟着变（不需要在 connect 路径挂钩子）
	declared, err := pm.Create("single", "COM7", SerialConfig{Port: "COM7", Baud: 9600}, nil, false)
	if err != nil {
		t.Fatalf("建声明进程失败: %v", err)
	}
	if got := declared.Name(); got != "COM7" {
		t.Fatalf("声明进程自动名 = %q，期望 COM7", got)
	}
	fwd, err := pm.Create("forward", "COM4", SerialConfig{Port: "COM4", Baud: 115200},
		&SerialConfig{Port: "COM14", Baud: 115200}, false)
	if err != nil {
		t.Fatalf("建转发进程失败: %v", err)
	}
	if got := fwd.Name(); got != "COM4 ↔ COM14" {
		t.Fatalf("转发进程自动名 = %q，期望 COM4 ↔ COM14", got)
	}

	// 改名 → 列表里能看到 name 与 nameCustom
	if err := pm.Rename(declared.id, "  取数口  "); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	entry := findEntry(t, pm.List(), declared.id)
	if entry["name"] != "取数口" {
		t.Fatalf("List 的 name = %v，期望 取数口（首尾空白应被去掉）", entry["name"])
	}
	if entry["nameCustom"] != true {
		t.Fatalf("List 的 nameCustom = %v，期望 true", entry["nameCustom"])
	}

	// 清空 → 回到自动名
	if err := pm.Rename(declared.id, "   "); err != nil {
		t.Fatalf("清空名字失败: %v", err)
	}
	if got := declared.Name(); got != "COM7" {
		t.Fatalf("清空后 = %q，期望回到自动名 COM7", got)
	}
	if declared.HasCustomName() {
		t.Fatal("清空后仍然报告有自定义名")
	}

	// 超长名字要拒绝
	if err := pm.Rename(declared.id, strings.Repeat("长", 65)); err == nil {
		t.Fatal("65 个字符的名字应当被拒绝")
	}
	// 不存在的进程要报错
	if err := pm.Rename("no-such-id", "x"); err == nil {
		t.Fatal("不存在的进程应当报错")
	}
}

// 工程会话改名要写回工程目录的 settings.json（这是唯一会持久化的名字）。
func TestRenamePersistsForProjectSession(t *testing.T) {
	dir := t.TempDir()
	sess, err := sessionfile.CreateSession(dir, sessionfile.Settings{
		Label: "COM9", Port: "COM9", Baud: 115200, Mode: sessionfile.ModeSingle,
	})
	if err != nil {
		t.Fatalf("建工程目录失败: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("关闭工程失败: %v", err)
	}

	pm := NewProcessManager()
	// 共享内存名字 = 实例 token + 进程号；同一测试二进制里再开一个
	// ProcessManager，进程号会从 1 重来，和上一个测试的环撞名。抬高起点避开。
	pm.counter.Store(900)
	proc, _, err := pm.OpenSession(dir)
	if err != nil {
		t.Fatalf("打开工程失败: %v", err)
	}
	// 文件里记的 label 就是名字（旧工程里是端口名）
	if got := proc.Name(); got != "COM9" {
		t.Fatalf("打开工程后名字 = %q，期望沿用 settings.json 的 COM9", got)
	}

	if err := pm.Rename(proc.id, "现场调试"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}

	// 目录名跟着名字走了：旧目录不在，新目录里 settings.json 拿到了新 label
	newDir := proc.sessionDir()
	if newDir == dir {
		t.Fatalf("目录没跟着改名，还是 %s", dir)
	}
	if base := filepath.Base(newDir); base != "现场调试" {
		t.Fatalf("新目录名 = %q，期望 现场调试", base)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("旧目录应该已经不在: %v", err)
	}

	doc, err := sessionfile.LoadSettingsDoc(newDir)
	if err != nil {
		t.Fatalf("读回 settings.json 失败: %v", err)
	}
	if doc.Label != "现场调试" {
		t.Fatalf("settings.json 的 label = %q，期望 现场调试", doc.Label)
	}
	// 端口等业务设置不能被改名冲掉
	if doc.Ports.A != "COM9" {
		t.Fatalf("改名把 ports.a 改坏了: %q", doc.Ports.A)
	}

	// 清空名字 → 目录名回到「时间_uuid8」，从 manifest 的身份重算
	if err := pm.Rename(proc.id, ""); err != nil {
		t.Fatalf("清空名字失败: %v", err)
	}
	back := proc.sessionDir()
	if back == newDir {
		t.Fatalf("清空名字后目录名应改回自动名，实际仍是 %s", back)
	}
	if m, err := sessionfile.LoadManifest(back); err != nil || m.SessionID == "" {
		t.Fatalf("自动名目录里读不到 manifest: %v", err)
	}
	if filepath.Dir(back) != filepath.Dir(dir) {
		t.Fatalf("目录没回到同一个父目录: %s", back)
	}
}

// 录制中改名：写者要重新挂到新目录上，前后数据一条不能少。
func TestRenameWhileRecording(t *testing.T) {
	histDir := t.TempDir()
	oldOverride := historyDirOverride
	historyDirOverride = histDir
	oldDefault := defaultSaveHistory()
	setDefaultSaveHistory(false)
	t.Cleanup(func() {
		historyDirOverride = oldOverride
		setDefaultSaveHistory(oldDefault)
	})

	pm := NewProcessManager()
	// 同一测试二进制里别的用例也建 ProcessManager，进程号都从 1 重来会让共享
	// 内存名撞在一起（报"共享内存已存在"）。抬高起点错开。
	pm.counter.Store(1901)
	t.Cleanup(pm.DestroyAll)
	proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil || proc == nil {
		t.Fatalf("建进程失败: %v", err)
	}

	proc.config = SerialConfig{Port: "COM9", Baud: 9600}
	proc.SetSaveHistory(true)

	proc.recordHistoryAt(1000, "AABB", "rx", "")
	proc.recordHistoryAt(1001, "CCDD", "tx", "")

	oldDir := onlySession(t, histDir)
	if proc.sessionDir() != "" {
		t.Fatalf("新录制的会话不该有 sessionPath: %q", proc.sessionDir())
	}

	if err := pm.Rename(proc.id, "现场调试"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	newDir := filepath.Join(histDir, "现场调试")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("新目录不存在: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("旧目录应该已经不在: %v", err)
	}
	proc.stlogMu.Lock()
	name := proc.stlogName
	proc.stlogMu.Unlock()
	if name != "现场调试" {
		t.Fatalf("stlogName = %q，期望跟着改名", name)
	}

	// 改名之后必须还能接着录
	proc.recordHistoryAt(1002, "EEFF", "rx", "")
	proc.detachHistoryFile()

	info := mustReadStlog(t, newDir)
	if len(info.Frames) != 3 {
		t.Fatalf("帧数 = %d, want 3", len(info.Frames))
	}
	if !bytes.Equal(info.Frames[2].Payload, []byte{0xEE, 0xFF}) {
		t.Fatalf("第 3 帧 = %X，期望 EEFF", info.Frames[2].Payload)
	}
	if info.SettingsDoc == nil || info.SettingsDoc.Label != "现场调试" {
		t.Fatalf("settings.json 的 label 没跟上: %+v", info.SettingsDoc)
	}
}

func findEntry(t *testing.T, list []map[string]any, id string) map[string]any {
	t.Helper()
	for _, e := range list {
		if e["processId"] == id {
			return e
		}
	}
	t.Fatalf("列表里找不到进程 %s", id)
	return nil
}
