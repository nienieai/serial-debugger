package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/protocol"
)

// newSaveTestProc 建一个只用于「历史落盘」测试的空闲进程，history 目录指向
// 临时目录（historyDirOverride），避免往测试二进制旁写 logs。
func newSaveTestProc(t *testing.T) (*ProcessManager, *Process, string) {
	t.Helper()
	dir := t.TempDir()
	oldDir := historyDirOverride
	historyDirOverride = dir
	oldDefault := defaultSaveHistory()
	setDefaultSaveHistory(false)
	t.Cleanup(func() {
		historyDirOverride = oldDir
		setDefaultSaveHistory(oldDefault)
	})

	pm := NewProcessManager()
	t.Cleanup(pm.DestroyAll)
	proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil || proc == nil {
		t.Fatalf("建进程失败: %v", err)
	}
	_ = pm.ClearHistory(proc.id)
	return pm, proc, dir
}

func countLogFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("读历史目录失败: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".log" {
			n++
		}
	}
	return n
}

// 默认不落盘：新进程即使有数据也不应产生会话目录。
func TestHistoryDefaultIsOff(t *testing.T) {
	pm, proc, dir := newSaveTestProc(t)

	if on, _ := pm.ProcessHistorySave(proc.id); on {
		t.Fatal("默认应为不保存")
	}
	proc.recordHistoryAt(1000, "AABB", "rx", "")
	proc.recordHistoryAt(1001, "CCDD", "tx", "")
	if n := len(findSessions(t, dir)); n != 0 {
		t.Fatalf("默认关闭时不应建会话目录, got %d 个", n)
	}
}

// 按进程开启后：立即建会话，后续数据追加；关闭后不再追加（已写入的保留）。
//
// v2 起「落盘」是一个会话目录（manifest + settings + pkg-*.stlog），
// 不再有 history/*.log —— 所以这里数目录、按封存后的记录数断内容。
func TestSetSaveHistoryPerProcess(t *testing.T) {
	pm, proc, dir := newSaveTestProc(t)

	if err := pm.SetProcessHistorySave(proc.id, true); err != nil {
		t.Fatalf("开启失败: %v", err)
	}
	if on, _ := pm.ProcessHistorySave(proc.id); !on {
		t.Fatal("开启后状态应为 true")
	}
	if n := len(findSessions(t, dir)); n != 1 {
		t.Fatalf("开启时应立即建 1 个会话目录, got %d", n)
	}

	proc.recordHistoryAt(1000, "AABB", "rx", "")
	// 封存会 Flush + Sync：不封存的话这一帧可能还留在页缓冲里，量不到。
	proc.detachHistoryFile()
	sess := onlySession(t, dir)
	if got := mustReadStlog(t, sess).Records; got != 1 {
		t.Fatalf("开启后数据应写进会话, got %d 条", got)
	}

	if err := pm.SetProcessHistorySave(proc.id, false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	proc.recordHistoryAt(1001, "CCDD", "tx", "")
	if n := len(findSessions(t, dir)); n != 1 {
		t.Fatalf("关闭后不该再建会话目录, got %d", n)
	}
	if got := mustReadStlog(t, sess).Records; got != 1 {
		t.Fatalf("关闭后不应再追加: 记录数 %d", got)
	}
	// 关掉文件不影响环形缓冲：内存里的历史仍在。
	if entries := historyOf(t, pm.GetHistoryPage(proc.id, 0, 0, 0)); len(entries) != 2 {
		t.Fatalf("环里应有 2 条, got %d", len(entries))
	}
}

// 默认值变化作用到「没单独设置过」的进程；显式设置过的保持原样。
func TestDefaultSaveHistoryAppliesOnlyToUnset(t *testing.T) {
	pm, proc, dir := newSaveTestProc(t)

	pm.SetDefaultHistorySave(true)
	if on, _ := pm.ProcessHistorySave(proc.id); !on {
		t.Fatal("默认改为开后，未单独设置的进程应跟着开")
	}
	if n := len(findSessions(t, dir)); n != 1 {
		t.Fatalf("跟着开时应建会话目录, got %d", n)
	}

	if err := pm.SetProcessHistorySave(proc.id, false); err != nil {
		t.Fatalf("按进程关闭失败: %v", err)
	}
	pm.SetDefaultHistorySave(false)
	pm.SetDefaultHistorySave(true)
	if on, _ := pm.ProcessHistorySave(proc.id); on {
		t.Fatal("显式关闭的进程不应被默认值重新打开")
	}

	proc2, err := pm.Create("single", "", SerialConfig{}, nil, false)
	if err != nil || proc2 == nil {
		t.Fatalf("建第二个进程失败: %v", err)
	}
	if on, _ := pm.ProcessHistorySave(proc2.id); !on {
		t.Fatal("新进程应继承默认值 true")
	}
}

// IPC 层：history.enable 带 processId 只影响该进程，status 带 processId 返回它。
func TestHistoryEnableIPCPerProcess(t *testing.T) {
	pm, proc, _ := newSaveTestProc(t)
	srv := NewIpcServer(pm)

	respMap := func(resp protocol.Response) map[string]any {
		t.Helper()
		if resp.Error != "" {
			t.Fatalf("IPC 返回错误: %s", resp.Error)
		}
		m, ok := resp.Result.(map[string]any)
		if !ok {
			t.Fatalf("Result 类型不对: %T", resp.Result)
		}
		return m
	}

	m := respMap(srv.dispatchForSession(nil, &protocol.Request{
		ID:     1,
		Method: string(contract.HistoryEnable),
		Params: map[string]any{"enabled": true, "processId": proc.id},
	}))
	if enabled, _ := m["enabled"].(bool); !enabled {
		t.Fatalf("返回 enabled=%v", m["enabled"])
	}

	m = respMap(srv.dispatchForSession(nil, &protocol.Request{
		ID:     2,
		Method: string(contract.HistoryStatus),
		Params: map[string]any{"processId": proc.id},
	}))
	if enabled, _ := m["enabled"].(bool); !enabled {
		t.Fatalf("按进程状态应为 true, got %v", m["enabled"])
	}

	// 全局默认仍是 false（按进程设置不应改默认值）。
	m = respMap(srv.dispatchForSession(nil, &protocol.Request{ID: 3, Method: string(contract.HistoryStatus)}))
	if enabled, _ := m["enabled"].(bool); enabled {
		t.Fatal("按进程设置不应改变全局默认值")
	}
}
