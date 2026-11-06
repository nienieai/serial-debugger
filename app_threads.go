package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
)

// ---- 新建会话：从已打开线程进入 ----
//
// 守护进程里的进程（界面上叫「线程」）是全局的，窗口只是登记「谁开着它」
// （见 app.go 的 sessOwner 与 GetSessionsForWindow）。这个入口把已经在跑、
// 但当前窗口没开着的会话接管过来：不重开串口，历史和统计都还在。
//
// 三种归属各走一条路：
//
//	无主      AdoptSession —— 直接认领
//	本窗口    前端自己切到已有的那个标签，不在 Go 侧登记任何东西
//	别的窗口  TakeSessionToWindow —— 转过来，源窗口摘标签、会话不销毁
//
// 归属表只存在于 Go 进程内存里，前端看不到，所以列表必须由 Go 侧带出
// owner / mine / free，前端才知道每一行该走哪条路。

// ListOpenSessions 列出守护进程上所有「可以从这里接手」的会话。
//
// online=false 表示守护进程没响应（和 fetchSessions 的 nil 语义一致）：
// 前端据此显示「守护进程未运行」，而不是「一个会话都没有」。
func (a *App) ListOpenSessions(ctx context.Context) map[string]any {
	win := winNameFromCtx(ctx)
	all := a.fetchSessions()
	if all == nil {
		return map[string]any{
			"online":   false,
			"self":     win,
			"sessions": []map[string]any{},
		}
	}
	return map[string]any{
		"online":   true,
		"self":     win,
		"sessions": annotateOpenSessions(all, win, a.sessWindow),
	}
}

// annotateOpenSessions 给每个会话标上归属，滤掉不该从这里进的，并按会话号排序。
//
// 拆成纯函数（owner 用回调喂进来）是为了能单测：这里是「哪一行能点、点了走
// 哪条路」的唯一判据，判错就会变成重复标签、或者两个窗口各关同一个会话一次。
func annotateOpenSessions(all []map[string]any, win string, ownerOf func(string) string) []map[string]any {
	out := make([]map[string]any, 0, len(all))
	for _, s := range all {
		sid, _ := s["id"].(string)
		if sid == "" {
			continue
		}
		// 工程会话（session.open 建出来的）自带一个文件页，从这里再挂一个会话页
		// 就成了同一份工程两份视图。它有自己的入口（拖入 / 从文件打开）。
		if p, _ := s["sessionPath"].(string); p != "" {
			continue
		}
		owner := ownerOf(sid)
		cp := make(map[string]any, len(s)+3)
		for k, v := range s {
			cp[k] = v
		}
		cp["owner"] = owner
		cp["mine"] = owner != "" && owner == win
		cp["free"] = owner == ""
		out = append(out, cp)
	}
	// 守护进程的进程表是 map，遍历顺序随机 —— 不排序的话每次刷新列表都在跳。
	sort.SliceStable(out, func(i, j int) bool {
		ai, _ := out[i]["id"].(string)
		aj, _ := out[j]["id"].(string)
		ni, errI := strconv.Atoi(ai)
		nj, errJ := strconv.Atoi(aj)
		if errI == nil && errJ == nil {
			return ni < nj
		}
		return ai < aj
	})
	return out
}

// AdoptSession 把一个「没人开着」的会话认领到当前窗口。
//
// 只做归属登记：串口、缓冲、历史都在守护进程里，本来就没动过。认领成功后
// 前端会在原地把选择页换成会话页（chooseOpenThread → _convertChooserToSession）。
func (a *App) AdoptSession(ctx context.Context, sessionID string) error {
	win := winNameFromCtx(ctx)
	if win == "" {
		return fmt.Errorf("取不到调用窗口，无法认领会话")
	}
	if sessionID == "" {
		return fmt.Errorf("会话 ID 为空")
	}
	if !a.sessClaim(sessionID, win) {
		if owner := a.sessWindow(sessionID); owner != "" {
			return fmt.Errorf("会话 %s 已经在窗口 %s 里打开了", sessionID, owner)
		}
		return fmt.Errorf("认领会话 %s 失败", sessionID)
	}
	// 显式认领可以覆盖「本窗口刚放手」的标记：用户就是要把它再挂回来。
	a.sessForgetReleased(sessionID)
	log.Printf("[window] 会话 %s 由窗口 %q 认领（从已打开线程进入）", sessionID, win)
	return nil
}

// ReleaseSession 把会话放回「无主」但**不销毁它**：串口继续开着、历史继续记。
//
// 这是「关闭标签 → 仅关闭标签」的落地：现在能从「从已打开线程进入」把它挂回来，
// 所以关标签不再等于断串口。放手的窗口自己不会立刻再领回来（见 sessReleased）。
func (a *App) ReleaseSession(ctx context.Context, sessionID string) error {
	win := winNameFromCtx(ctx)
	if win == "" {
		return fmt.Errorf("取不到调用窗口，无法释放会话")
	}
	if sessionID == "" {
		return fmt.Errorf("会话 ID 为空")
	}
	if !a.sessRelease(sessionID, win) {
		return fmt.Errorf("会话 %s 不归这个窗口", sessionID)
	}
	log.Printf("[window] 会话 %s 由窗口 %q 放回无主（仅关闭标签）", sessionID, win)
	return nil
}

// TakeSessionToWindow 把别的窗口正开着的会话转到当前窗口。
//
// 和拖标签走同一条路：**只转归属，不销毁会话**。源窗口收到 session-taken 帧后
// 把标签摘掉（app.js 里 _detachTab），串口照旧开着、历史照旧在；它要是因此
// 一个标签都不剩，会自己回到「空窗口」的正常流程（补一个空闲会话）。
//
// 这里不发 sessions-changed：目标窗口是**本地原地接管**（不等广播），源窗口由
// 这帧直接处理。多发一次广播只会让目标窗口在标签还没挂上时先建一个出来。
func (a *App) TakeSessionToWindow(ctx context.Context, sessionID string) error {
	win := winNameFromCtx(ctx)
	if win == "" {
		return fmt.Errorf("取不到调用窗口，无法接管会话")
	}
	if sessionID == "" {
		return fmt.Errorf("会话 ID 为空")
	}
	old := a.sessWindow(sessionID)
	if old == win {
		return nil
	}
	a.sessSet(sessionID, win)
	if old != "" {
		a.sendFrame(old, map[string]any{
			"e": "session-taken",
			"d": map[string]any{"sid": sessionID, "to": win},
		})
	}
	log.Printf("[window] 会话 %s 从窗口 %q 转到 %q（从已打开线程进入）", sessionID, old, win)
	return nil
}
