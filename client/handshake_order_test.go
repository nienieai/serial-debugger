package client

import (
	"bufio"
	"testing"
	"time"

	"github.com/nienieai/serial-debugger/pipe"
	"github.com/nienieai/serial-debugger/protocol"
)

// TestHandshakeConfirmationBeforeRespPipeIsNotAFailure 是 v0.7.4.2 那个回归的守卫。
//
// 为什么重要：守护进程在**两条回连管道都成功之后**才写注册确认
// （daemon/ipc.go:532，位于 addSession 之后）。所以确认消息可能抢在客户端
// Accept 协程把 respConn/subConn 交出来之前到达——负载越重、机器越慢，这个
// 窗口越大。
//
// v0.7.4 引入的「监听 daemonConn 以透传失败原因」把这一点做成了会误判的路径：
// 当时用 select 同时等 respCh 与 earlyCh，谁先到就按谁处理，于是**先到的成功
// 确认被判成了握手失败**。实测现象极具误导性——守护进程日志显示已完成注册、
// 没有任何回连失败，客户端却报「注册未完成」，且三次重试全在 2ms 内一起失败
// （同一个竞态被连续触发，所以重试完全无效）。外部测试机上因此有 6.3%~11% 的
// 用户可见失败率，且随机器变慢而升高。
//
// 本测试把确认消息**刻意提前**送出，并让 resp/sub 管道晚 300ms 才连上。
// 断言：客户端必须成功，而不是因为「确认先到」就报失败。
func TestHandshakeConfirmationBeforeRespPipeIsNotAFailure(t *testing.T) {
	ln, err := pipe.Listen(pipe.Addr)
	if err != nil {
		t.Skip("守护进程端点已被占用（可能有真实守护进程在运行），跳过")
	}
	defer ln.Close()

	// 假守护进程：收到 register 后立刻回「已注册」确认，**然后**才回连
	// 客户端的 resp/sub 管道（经 300ms 延迟放大竞态窗口）。
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				data, err := protocol.ReadMessage(r)
				if err != nil {
					return
				}
				req, err := protocol.ParseRequest(data)
				if err != nil {
					return
				}
				clientID, _ := req.Params["clientId"].(string)
				respPipe, _ := req.Params["respPipe"].(string)
				subPipe, _ := req.Params["subPipe"].(string)
				if respPipe == "" || subPipe == "" {
					return
				}

				// 1) 先送确认——模拟「确认抢在管道就绪之前」
				_ = protocol.WriteMessage(conn, protocol.Response{
					ID:     req.ID,
					Result: map[string]any{"registered": true, "clientId": clientID},
				})

				// 2) 延迟后再回连两条管道
				time.Sleep(300 * time.Millisecond)
				respConn, err := pipe.Dial(respPipe)
				if err != nil {
					return
				}
				defer respConn.Close()
				subConn, err := pipe.Dial(subPipe)
				if err != nil {
					return
				}
				defer subConn.Close()

				// 保持会话存活，让客户端的 verifyProtocol 能拿到响应。
				for {
					d, err := protocol.ReadMessage(r)
					if err != nil {
						return
					}
					req, err := protocol.ParseRequest(d)
					if err != nil {
						continue
					}
					_ = protocol.WriteMessage(respConn, protocol.Response{
						ID: req.ID,
						Result: protocol.DaemonInfo{
							Version:         "test",
							ProtocolVersion: protocol.ProtocolVersion,
							PID:             1,
							StartTime:       time.Now().Format(time.RFC3339),
						},
					})
				}
			}()
		}
	}()

	old := handshakeTimeout
	handshakeTimeout = 3 * time.Second
	defer func() { handshakeTimeout = old }()

	c, err := NewDaemonClient("test-confirm-early")
	if err != nil {
		t.Fatalf("注册确认先到不应导致握手失败，实际失败: %v", err)
	}
	c.Close()
}
