package main

import "testing"

func TestDecideClose(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		connected bool
		clients   int
		want      closeAction
	}{
		{"never：从不处理", "never", true, 1, closeProceed},
		{"never：有多个客户端也不动", "never", true, 5, closeProceed},
		{"缺键（旧 gui.ini）：回落 never", "", true, 1, closeProceed},

		{"last：自己是最后一个客户端 → 直接关（不询问）", "last", true, 1, closeStopDaemon},
		{"last：还有别的客户端 → 不动", "last", true, 2, closeProceed},
		{"last：还有 9 个客户端 → 不动", "last", true, 9, closeProceed},

		{"always：每次关窗口都关（不询问）", "always", true, 1, closeStopDaemon},
		{"always：还有别的客户端也照关", "always", true, 5, closeStopDaemon},

		// 0.9.1 的第 4 档：不管自己是第几个客户端，先问一句再决定。
		{"ask：自己是最后一个客户端也要问", "ask", true, 1, closeConfirm},
		{"ask：还有别的客户端也问", "ask", true, 5, closeConfirm},

		{"未连接守护进程 → 直接放行（always）", "always", false, 0, closeProceed},
		{"未连接守护进程 → 直接放行（ask）", "ask", false, 0, closeProceed},
		{"未连接守护进程 → 直接放行（last）", "last", false, 0, closeProceed},

		{"认不出来的档位 → 不动守护进程", "bogus", true, 1, closeProceed},
	}

	for _, c := range cases {
		got := decideClose(c.mode, c.connected, c.clients)
		if got != c.want {
			t.Errorf("%s: decideClose(%q, conn=%v, n=%d) = %v, want %v",
				c.name, c.mode, c.connected, c.clients, got, c.want)
		}
	}
}

// 最后一个窗口关掉时会话怎么处理：守护进程要停就一起销毁，不停就放回无主。
// 这一档不再问线程（见「关闭窗口流程」文档）。
func TestLastWindowCloseMode(t *testing.T) {
	cases := []struct {
		action closeAction
		want   string
	}{
		{closeStopDaemon, "process"},
		{closeConfirm, "process"},
		{closeProceed, "tab"},
	}
	for _, c := range cases {
		if got := lastWindowCloseMode(c.action); got != c.want {
			t.Errorf("lastWindowCloseMode(%v) = %q, want %q", c.action, got, c.want)
		}
	}
}

// 0.9.1 之前的「关闭 GUI 前确认」布尔键要能读出来，才能迁成 ask 这一档。
func TestLegacyDaemonWarn(t *testing.T) {
	cases := []struct {
		name   string
		data   string
		want   bool
		wantOK bool
	}{
		{"没有这个键", "[display]\nshutdown_daemon_mode = last\n", false, false},
		{"旧配置开着提醒", "[display]\nshutdown_daemon_warn = true\n", true, true},
		{"旧配置关着提醒", "[display]\nshutdown_daemon_warn = false\n", false, true},
		{"带 BOM 的首行", "\ufeffshutdown_daemon_warn = true\n", true, true},
		{"被注释掉的键不算", "#shutdown_daemon_warn = true\n", false, false},
		{"值写坏了当没有", "shutdown_daemon_warn = yes please\n", false, false},
	}
	for _, c := range cases {
		got, ok := legacyDaemonWarn([]byte(c.data))
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: legacyDaemonWarn = (%v, %v), want (%v, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

// 守护进程停了之后「会话」也全没了，归属表必须一起清空——
// 否则关窗口时还会按旧会话数弹「会停止这个窗口里的会话（共 3 个）」，
// 而界面其实已经显示「守护进程未启用」（2026-11-02 实测）。
func TestSessClearAll(t *testing.T) {
	a := &App{sessOwner: map[string]string{"1": "main", "2": "win-1", "3": "main"}}
	if got := len(a.sessOfWindow("main")); got != 2 {
		t.Fatalf("准备阶段：main 应有 2 个会话，实际 %d", got)
	}
	a.sessClearAll()
	if n := len(a.sessOfWindow("main")) + len(a.sessOfWindow("win-1")); n != 0 {
		t.Fatalf("sessClearAll 后归属应清空，实际还剩 %d", n)
	}
	if windowCloseNeedsConfirm("ask", len(a.sessOfWindow("main"))) {
		t.Error("清空后按会话数判定不应再弹确认框")
	}
}
func TestWindowCloseNeedsConfirm(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		sessions   int
		want       bool
	}{
		{"每次询问：有会话 → 问", "ask", 2, true},
		{"每次询问：没会话 → 不问", "ask", 0, false},
		{"仅关闭标签/窗口：有会话也不再问", "tab", 2, false},
		{"仅关闭标签/窗口：没会话 → 不问", "tab", 0, false},
		{"连线程一起关：有会话也不再问", "process", 2, false},
		{"连线程一起关：没会话 → 不问", "process", 0, false},
		{"缺键/旧配置按每次询问算", "", 1, true},
		// 0.9.1 删掉了 windowCloseAction（keep / destroy / transfer 都不再存在）。
		// LoadAppSettings 会把非法值归一成 ask，这里再兜一层：认不出来的值一律
		// 按「问一次」，绝不当成 tab/process 直接执行。
		{"旧窗口档位的值 → 问", "keep", 2, true},
		{"旧的 transfer 值 → 问", "transfer", 2, true},
	}
	for _, c := range cases {
		if got := windowCloseNeedsConfirm(c.configured, c.sessions); got != c.want {
			t.Errorf("%s: windowCloseNeedsConfirm(%q, %d) = %v, want %v",
				c.name, c.configured, c.sessions, got, c.want)
		}
	}
}
