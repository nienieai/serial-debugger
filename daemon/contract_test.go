package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/protocol"
)

// TestEveryContractMethodIsDispatched 校验 contract 声明的每个方法在 daemon 中
// 都真的被处理，而不是落到 dispatchForSession 的 default 分支返回 "unknown method"。
//
// 契约只有在「声明即实现」时才可信：此前方法名散落在六处裸字符串里，已经出现
// multistr.status 这种 daemon 根本不存在、却被 CLI 与 MCP 同时使用的名字。
//
// 用「能快速失败的最小参数」逐个调用：本测试只关心是否被实现，不关心业务结果，
// 因此缺参数的调用返回什么错误都算通过。
func TestEveryContractMethodIsDispatched(t *testing.T) {
	srv := NewIpcServer(NewProcessManager())
	defer srv.Shutdown()

	// skip 里是本测试不能或不应真正执行的方法。
	skip := map[contract.Method]string{
		// 正常行为就是终止进程（100ms 后 os.Exit(0)）
		contract.Shutdown: "会终止测试进程",
	}
	// params 用来避免副作用过大的默认行为。
	params := map[contract.Method]map[string]any{
		// 不指定端口时它会探测全部端口（5 端口 × 7 波特率 × 3 规则），
		// 单次调用可达数分钟。给一个不存在的端口让它立刻返回。
		contract.PortsProbe: {"ports": []any{"__probe_nonexistent__"}},
	}

	for _, m := range contract.All {
		if reason, ok := skip[m]; ok {
			t.Run(string(m), func(t *testing.T) {
				t.Skipf("跳过执行：%s", reason)
			})
			continue
		}
		t.Run(string(m), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("方法 %s 以裸请求调用时 panic: %v", m, r)
				}
			}()
			resp := srv.dispatchForSession(nil, &protocol.Request{
				ID: 1, Method: string(m), Params: params[m],
			})
			if strings.HasPrefix(resp.Error, protocol.UnknownMethodPrefix) {
				t.Errorf("契约声明的方法 %s 在 daemon 中未实现", m)
			}
		})
	}
}

// TestDispatchHasNoLiteralMethodNames 守住「契约是唯一权威来源」这件事。
//
// 只要有人在 dispatch 里把某个 case 写回字面量，契约就重新变成一份可能与实现
// 脱节的平行清单，改名时也会漏掉它。
func TestDispatchHasNoLiteralMethodNames(t *testing.T) {
	src, err := os.ReadFile("ipc.go")
	if err != nil {
		t.Fatalf("读取 ipc.go: %v", err)
	}
	text := string(src)

	for _, m := range contract.All {
		literal := `case "` + string(m) + `":`
		if strings.Contains(text, literal) {
			t.Errorf("ipc.go 中的 `case %q` 应改用 contract 常量", string(m))
		}
	}
}

// TestContractListIsWellFormed 校验契约清单自身没有明显错误。
func TestContractListIsWellFormed(t *testing.T) {
	if len(contract.All) == 0 {
		t.Fatal("contract.All 为空")
	}

	seen := make(map[contract.Method]bool, len(contract.All))
	dup := false
	for _, m := range contract.All {
		if m == "" {
			t.Error("contract.All 含空方法名")
		}
		if strings.TrimSpace(string(m)) != string(m) {
			t.Errorf("方法名含首尾空白: %q", m)
		}
		if seen[m] {
			t.Errorf("contract.All 重复出现: %s", m)
			dup = true
		}
		seen[m] = true
	}
	if dup {
		t.Fatal("contract.All 存在重复项")
	}

	// 每个方法名都应当是小写点分风格
	pattern := regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`)
	for _, m := range contract.All {
		if !pattern.MatchString(string(m)) {
			t.Errorf("方法名不符合点分小写约定: %s", m)
		}
	}
}

// TestContractValid 校验 Valid 的边界行为。
func TestContractValid(t *testing.T) {
	if !contract.Valid(contract.ProcessCreate) {
		t.Error("ProcessCreate 应当有效")
	}
	if contract.Valid("process.does_not_exist") {
		t.Error("未声明的方法不应被判为有效")
	}
	if contract.Valid("multistr.status") {
		t.Error("multistr.status 是历史上出现过的不存在的方法名，不应被判为有效")
	}
}
