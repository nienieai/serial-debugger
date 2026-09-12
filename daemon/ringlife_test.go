package main

import (
	"sync"
	"testing"
	"time"
)

// TestRingLifetimeRace 复现「进程已从 pm.processes 摘除，但指针仍被并发请求
// 持有」这一窗口：pm.Close() 在释放 pm.mu 之后才调用 closeHistory()，于是
// 读取方可能在 closeHistory 已经把共享内存 unmap 之后才去碰环。
//
// 为什么重要：dispatchForSession 路径上没有任何 recover，而 historyRing 是
// MapViewOfFile 映射——在已撤销的映射上取快照是访问违规（0xC0000005），会让
// 整个守护进程退出、所有串口会话断开。sendRing 的同名窗口则表现为对 nil 调用
// 方法而 panic。
//
// 这个测试在**修复前必须失败**（`go test -race` 下报 DATA RACE，或直接 panic），
// 修复后必须通过。回归价值全部来自这个前后差异，因此不要在没有 -race 的情况下
// 把它当作验证通过。
func TestRingLifetimeRace(t *testing.T) {
	// 关掉自动落盘：本测试只关心环的生命周期，historyDir() 会指向测试二进制
	// 所在目录，开着会往那里堆 *.log。
	setHistoryEnabled(false)
	defer setHistoryEnabled(true)

	pm := NewProcessManager()
	defer pm.DestroyAll()

	const rounds = 3000

	var wg sync.WaitGroup

	// 写者：持续往历史环里灌数据，同时不断触发惰性建文件路径。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds*4; i++ {
			pm.mu.RLock()
			procs := make([]*Process, 0, len(pm.processes))
			for _, p := range pm.processes {
				procs = append(procs, p)
			}
			pm.mu.RUnlock()
			for _, p := range procs {
				p.recordHistory("DEADBEEF", "rx")
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()

	// 读者：取到指针后立刻用，制造「指针有效但环已被关闭」的交错。
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds*4; i++ {
				pm.mu.RLock()
				ids := make([]string, 0, len(pm.processes))
				for id := range pm.processes {
					ids = append(ids, id)
				}
				pm.mu.RUnlock()
				for _, id := range ids {
					_ = pm.GetHistory(id)
					_ = pm.ClearHistory(id)
					_, _ = pm.MultistrReadEntries(id)
					_ = pm.SendTrigger(id, true)
				}
			}
		}()
	}

	// 建/销：不断重复 closeHistory + closeSendRing。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			proc, err := pm.Create("single", "", SerialConfig{}, nil, false)
			if err != nil || proc == nil {
				continue
			}
			// 不 sleep：让建/销保持连续，最大化与读者交错的机会。
			_ = pm.Destroy(proc.id)
		}
	}()

	wg.Wait()
}
