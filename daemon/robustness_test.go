package main

import (
	"strings"
	"testing"

	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/protocol"
)

// TestMalformedParamsDoNotPanic 用各种畸形参数调用每个方法，确认没有任何一条
// 能把守护进程打崩。
//
// 为什么重要：dispatchForSession 路径上没有任何 recover，一次 panic 就是整个
// 守护进程退出 —— 所有会话断开、所有串口关闭。而 IPC 是对任何本机进程开放的，
// 参数完全由调用方提供，因此「畸形参数会不会 panic」是必须守住的边界。
//
// 参数不是通过类型化的解码进入 handler 的：convParams 走 JSON 往返，多数情况
// 会返回错误；但仍有直接对 req.Params 做类型断言的地方，这类地方一旦没有逗号-ok
// 形式就会 panic。
func TestMalformedParamsDoNotPanic(t *testing.T) {
	srv := NewIpcServer(NewProcessManager())
	defer srv.Shutdown()
	// process.create / forward.create 即使参数畸形也会真的建出空闲进程，
	// 而每个进程持有 5MB 历史环 + 1MB 发送队列，必须回收。
	defer srv.pm.DestroyAll()

	// 这些取值覆盖常见的错误类型：字符串型字段收到对象/数组/数字，数字型字段
	// 收到字符串，以及 json.Unmarshal 到结构体会失败的各种形状。
	hostile := []map[string]any{
		{"processId": map[string]any{"nested": true}},
		{"processId": []any{1, 2, 3}},
		{"processId": 12345},
		{"processId": nil},
		{"ports": "COM3"},
		{"ports": map[string]any{}},
		{"ports": []any{1, 2, 3}},
		{"data": map[string]any{}},
		{"data": []any{}},
		{"file": 42},
		{"keyword": []any{}},
		{"limit": "many"},
		{"intervalMs": "fast"},
		{"enabled": "yes"},
		{"baud": map[string]any{}},
		{"mode": []any{}},
		{"configPath": 7},
		{"entries": "not-an-array"},
		{"rules": map[string]any{}},
		{"baudRates": "9600"},
	}

	skip := map[contract.Method]string{
		contract.Shutdown: "会终止测试进程",
	}
	// ports.probe 缺省会探测全部端口；用畸形参数时它多半在解码阶段就返回，
	// 但为稳妥仍给它一个不存在的端口名。
	safe := map[contract.Method]map[string]any{
		contract.PortsProbe: {"ports": []any{"__probe_nonexistent__"}},
	}

	panics := 0
	for _, m := range contract.All {
		if _, ok := skip[m]; ok {
			continue
		}
		for i, params := range hostile {
			func() {
				defer func() {
					if r := recover(); r != nil {
						panics++
						t.Errorf("方法 %s 收到畸形参数 #%d 时 panic: %v\n  params=%v",
							m, i, r, params)
					}
				}()
				p := params
				if s, ok := safe[m]; ok {
					p = s
				}
				_ = srv.dispatchForSession(nil, &protocol.Request{
					ID: 1, Method: string(m), Params: p,
				})
			}()
		}
	}
	if panics == 0 {
		t.Log("全部方法 × 20 组畸形参数均未 panic")
	}
}

// TestUnknownMethodIsRejected 确认未声明的方法被明确拒绝，且错误信息里带上
// 方法名（否则排查时不知道是哪个方法）。
func TestUnknownMethodIsRejected(t *testing.T) {
	srv := NewIpcServer(NewProcessManager())
	defer srv.Shutdown()

	resp := srv.dispatchForSession(nil, &protocol.Request{
		ID: 1, Method: "no.such.method", Params: map[string]any{},
	})
	if resp.Error == "" {
		t.Fatal("未声明的方法应当返回错误")
	}
	if !strings.HasPrefix(resp.Error, protocol.UnknownMethodPrefix) {
		t.Fatalf("错误信息应以 %q 开头, got: %s", protocol.UnknownMethodPrefix, resp.Error)
	}
	if !strings.Contains(resp.Error, "no.such.method") {
		t.Fatalf("错误信息应包含方法名, got: %s", resp.Error)
	}
}
