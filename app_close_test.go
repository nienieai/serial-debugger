package main

import "testing"

func TestDecideClose(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		warn      bool
		connected bool
		clients   int
		want      closeAction
	}{
		{"never：从不处理", "never", true, true, 1, closeProceed},
		{"never：有多个客户端也不动", "never", true, true, 5, closeProceed},
		{"缺键（旧 gui.ini）：回落 never", "", true, true, 1, closeProceed},

		{"last：自己是最后一个 + 无警告 → 直接关", "last", false, true, 1, closeStopDaemon},
		{"last：自己是最后一个 + 有警告 → 弹确认", "last", true, true, 1, closeConfirm},
		{"last：还有别的客户端 → 不动（无警告）", "last", false, true, 2, closeProceed},
		{"last：还有别的客户端 → 不动（有警告，也不该弹）", "last", true, true, 2, closeProceed},
		{"last：还有 9 个客户端 → 不动", "last", true, true, 9, closeProceed},

		{"always：无警告 → 直接关", "always", false, true, 1, closeStopDaemon},
		{"always：有警告 → 弹确认", "always", true, true, 1, closeConfirm},
		{"always：还有别的客户端也照关（无警告）", "always", false, true, 5, closeStopDaemon},
		{"always：还有别的客户端也照关（有警告）", "always", true, true, 5, closeConfirm},

		{"未连接守护进程 → 直接放行", "always", true, false, 0, closeProceed},
		{"未连接守护进程（last）→ 直接放行", "last", true, false, 0, closeProceed},
	}

	for _, c := range cases {
		got := decideClose(c.mode, c.warn, c.connected, c.clients)
		if got != c.want {
			t.Errorf("%s: decideClose(%q, warn=%v, conn=%v, n=%d) = %v, want %v",
				c.name, c.mode, c.warn, c.connected, c.clients, got, c.want)
		}
	}
}
