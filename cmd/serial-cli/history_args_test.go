package main

import "testing"

// `history` 的分页参数解析：位置参数与三个选项可以混用，未知选项必须报错。
func TestParseHistoryArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, h historyArgs)
	}{
		{
			name: "空参数 → 用默认进程、不限量",
			args: nil,
			check: func(t *testing.T, h historyArgs) {
				if len(h.positional) != 0 || h.limit != 0 || h.beforeMs != 0 || h.skip != 0 {
					t.Fatalf("期望全默认, got %+v", h)
				}
			},
		},
		{
			name: "只有进程号",
			args: []string{"3"},
			check: func(t *testing.T, h historyArgs) {
				if len(h.positional) != 1 || h.positional[0] != "3" {
					t.Fatalf("期望进程号 3, got %+v", h)
				}
			},
		},
		{
			name: "进程号在前、选项在后",
			args: []string{"3", "--limit", "50"},
			check: func(t *testing.T, h historyArgs) {
				if h.limit != 50 || h.positional[0] != "3" {
					t.Fatalf("期望 limit=50 进程 3, got %+v", h)
				}
			},
		},
		{
			name: "选项在前、进程号在后",
			args: []string{"--limit", "50", "--before", "1750000000000", "--skip", "2", "3"},
			check: func(t *testing.T, h historyArgs) {
				if h.limit != 50 || h.beforeMs != 1750000000000 || h.skip != 2 {
					t.Fatalf("游标参数解析错误: %+v", h)
				}
				if len(h.positional) != 1 || h.positional[0] != "3" {
					t.Fatalf("进程号应当仍是 3: %+v", h)
				}
			},
		},
		{name: "未知选项必须报错", args: []string{"--json"}, wantErr: true},
		{name: "拼错的选项必须报错", args: []string{"--limitt", "5"}, wantErr: true},
		{name: "缺值 --limit", args: []string{"--limit"}, wantErr: true},
		{name: "缺值 --before", args: []string{"--before"}, wantErr: true},
		{name: "缺值 --skip", args: []string{"--skip"}, wantErr: true},
		{name: "非数字 --limit", args: []string{"--limit", "abc"}, wantErr: true},
		{name: "零 --limit", args: []string{"--limit", "0"}, wantErr: true},
		{name: "负 --limit", args: []string{"--limit", "-5"}, wantErr: true},
		{name: "非数字 --before", args: []string{"--before", "abc"}, wantErr: true},
		{name: "负数 --skip", args: []string{"--skip", "-1"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHistoryArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际通过: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望通过，实际报错: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}
