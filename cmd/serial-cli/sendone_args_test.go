package main

// sendone 的参数解析：位置参数是进程号，另有 --raw。
//
// 这条命令存在的理由（外部测试报告 0.7.5.7 轮 §8）：`send.trigger{raw:false}`
// 是「多字符串条目剥头」那条修复所在的路径，而它此前**没有任何黑盒入口**
// —— CLI 没有命令、MCP 只有 raw:true、GUI 的 App.TriggerSend 没被调用。
// 于是那条修复在验收上不可证。补上这个入口后，队列里放一条 `delay: 1` 的条目，
// 用 sendone 发一次即可在接收端看出「发的是内容还是内容+5 字节二进制头」。
//
// 这里只测参数解析（不需要守护进程）：把 --raw 与进程号的位置组合固定下来。

import "testing"

func TestParseSendoneArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantRaw bool
		wantPid string
		wantErr bool
	}{
		{name: "只有 --raw", args: []string{"--raw"}, wantRaw: true, wantPid: ""},
		{name: "只有进程号", args: []string{"3"}, wantRaw: false, wantPid: "3"},
		{name: "进程号在前", args: []string{"3", "--raw"}, wantRaw: true, wantPid: "3"},
		{name: "进程号在后", args: []string{"--raw", "3"}, wantRaw: true, wantPid: "3"},
		{name: "空的", args: nil, wantRaw: false, wantPid: ""},
		{name: "未知参数", args: []string{"--json"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, pid, err := parseSendoneArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际通过: raw=%v pid=%q", raw, pid)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
			if raw != tc.wantRaw || pid != tc.wantPid {
				t.Fatalf("解析结果不对: raw=%v pid=%q，期望 raw=%v pid=%q",
					raw, pid, tc.wantRaw, tc.wantPid)
			}
		})
	}
}
