package main

import (
	"context"
	"testing"
)

// annotateOpenSessions 是「从已打开线程进入」列表的唯一判据：某一行能不能点、
// 点了是认领还是转窗口，全看它标出来的 owner/mine/free。判错的后果是重复标签
// 或者两个窗口各关同一个会话一次，所以逐条钉住。
func TestAnnotateOpenSessions(t *testing.T) {
	owners := map[string]string{"2": "main", "3": "win-2"}
	ownerOf := func(sid string) string { return owners[sid] }
	all := []map[string]any{
		{"id": "1", "status": "connected", "portName": "COM3"},
		{"id": "2", "status": "idle"},
		{"id": "3", "status": "connected", "portName": "COM4"},
		{"id": "4", "status": "idle", "sessionPath": `C:\proj`},
		{"status": "idle"}, // 没有 id 的脏数据
	}

	got := annotateOpenSessions(all, "main", ownerOf)

	if len(got) != 3 {
		t.Fatalf("应只剩 3 条（工程会话与会话号为空都要滤掉），得到 %d: %v", len(got), got)
	}
	// 按会话号排序：守护进程的进程表是 map，不排序列表每次刷新都在跳
	for i, want := range []string{"1", "2", "3"} {
		if got[i]["id"] != want {
			t.Fatalf("第 %d 条应是会话 %s，得到 %v", i, want, got[i]["id"])
		}
	}

	// 会话 1：无主
	if got[0]["free"] != true || got[0]["mine"] != false || got[0]["owner"] != "" {
		t.Fatalf("会话 1 应判为无主，得到 owner=%v mine=%v free=%v",
			got[0]["owner"], got[0]["mine"], got[0]["free"])
	}
	// 会话 2：本窗口的
	if got[1]["mine"] != true || got[1]["free"] != false || got[1]["owner"] != "main" {
		t.Fatalf("会话 2 应判为本窗口，得到 owner=%v mine=%v free=%v",
			got[1]["owner"], got[1]["mine"], got[1]["free"])
	}
	// 会话 3：别的窗口的 —— 只能走「转到本窗口」
	if got[2]["mine"] != false || got[2]["free"] != false || got[2]["owner"] != "win-2" {
		t.Fatalf("会话 3 应判为 win-2 的，得到 owner=%v mine=%v free=%v",
			got[2]["owner"], got[2]["mine"], got[2]["free"])
	}

	// 原始列表不能被就地改写：fetchSessions 的返回值另外还有人在用
	if _, ok := all[0]["owner"]; ok {
		t.Fatal("annotateOpenSessions 不应改写入参里的 map")
	}
}

// 没有来源窗口（脚本/内部调用）时不能把任何会话误判成本窗口的。
func TestAnnotateOpenSessionsNoWindow(t *testing.T) {
	all := []map[string]any{{"id": "1"}}
	got := annotateOpenSessions(all, "", func(string) string { return "main" })
	if len(got) != 1 {
		t.Fatalf("应有 1 条，得到 %d", len(got))
	}
	if got[0]["mine"] != false {
		t.Fatal("窗口名为空时不能把会话判成本窗口的")
	}
}

// 「仅关闭标签」= 把会话放回无主但线程还在。放手之后：
//   - 归属表里没有它了（别的窗口能在列表里看到并认领）
//   - 但放手方自己被记下来，GetSessionsForWindow 的「无主就认领」要跳过它，
//     否则标签一关就被自己领回来，等于关不掉
func TestSessReleaseKeepsSessionAndMarksReleaser(t *testing.T) {
	var a App
	if !a.sessClaim("7", "main") {
		t.Fatal("初次认领应当成功")
	}
	if !a.sessRelease("7", "main") {
		t.Fatal("自己的会话应当能放手")
	}
	if got := a.sessWindow("7"); got != "" {
		t.Fatalf("放手后应当无主，得到 %q", got)
	}
	if got := a.sessReleasedFor("7"); got != "main" {
		t.Fatalf("应当记住是 main 放的，得到 %q", got)
	}
	// 别的窗口释放不了不归它的会话
	if a.sessRelease("7", "win-2") {
		t.Fatal("不归它的会话不该允许释放")
	}
	// 显式认领（从列表里点回来）要能覆盖放手标记
	if !a.sessClaim("7", "main") {
		t.Fatal("显式认领应当允许")
	}
	a.sessForgetReleased("7")
	if got := a.sessReleasedFor("7"); got != "" {
		t.Fatalf("认领后放手标记应清掉，得到 %q", got)
	}
}

// 「关闭窗口 → 仅关闭窗口（线程保留）」：窗口名下的会话全部放回无主，
// 别的窗口的会话一个都不能动。
func TestReleaseWindowSessions(t *testing.T) {
	a := &App{sessOwner: map[string]string{"1": "win-2", "2": "win-2", "3": "main"}}
	if n := a.releaseWindowSessions("win-2"); n != 2 {
		t.Fatalf("应当放手 2 个会话，实际 %d", n)
	}
	for _, sid := range []string{"1", "2"} {
		if got := a.sessWindow(sid); got != "" {
			t.Fatalf("会话 %s 放手后应当无主，得到 %q", sid, got)
		}
		if got := a.sessReleasedFor(sid); got != "win-2" {
			t.Fatalf("会话 %s 应当记住是 win-2 放的，得到 %q", sid, got)
		}
	}
	if got := a.sessWindow("3"); got != "main" {
		t.Fatalf("别的窗口的会话不该被放手，得到 %q", got)
	}
	// 再放一次不会有第二个（已经无主了）
	if n := a.releaseWindowSessions("win-2"); n != 0 {
		t.Fatalf("重复放手应当返回 0，实际 %d", n)
	}
}

// 认领/接管都必须先拿到调用窗口，否则后端会把会话登记到一个空名字上。
func TestAdoptAndTakeRequireWindow(t *testing.T) {
	var a App
	if err := a.AdoptSession(context.Background(), "1"); err == nil {
		t.Fatal("没有窗口上下文时认领应当报错")
	}
	if err := a.TakeSessionToWindow(context.Background(), "1"); err == nil {
		t.Fatal("没有窗口上下文时接管应当报错")
	}
	// 会话 ID 为空也要拦在改归属之前
	if err := a.AdoptSession(context.Background(), ""); err == nil {
		t.Fatal("会话 ID 为空时认领应当报错")
	}
}
