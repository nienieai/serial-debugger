package main

import (
	"os"
	"path/filepath"
	"testing"
)

// probe.toml 的查找必须与启动时的 cwd 无关——发布包里 exe 与 config 子目录
// 同级，若只依赖 cwd，走快捷方式或从别的目录调用 serial-cli 时随包的
// config/probe.toml 就会被静默忽略，退回内置规则。
func TestProbeConfigCandidatesArchiveLayout(t *testing.T) {
	exeDir := filepath.Join(`C:\`, "Embedded", "串口调试工具", "0.6.5")
	wd := filepath.Join(`C:\`, "Windows", "Temp")

	got := probeConfigCandidates(exeDir, wd)

	want := filepath.Join(exeDir, "config", "probe.toml")
	if !containsPath(got, want) {
		t.Fatalf("候选中缺少 <exe>/config/probe.toml\nwant: %s\ngot:  %v", want, got)
	}
}

// 优先级：exe 旁的 probe.toml > exe/config/probe.toml > exe/../config/... >
// cwd 下的两份。越靠前越优先。
func TestProbeConfigCandidatesPriority(t *testing.T) {
	exeDir := filepath.Join("D:", "app", "bin")
	wd := filepath.Join("D:", "work")

	want := []string{
		filepath.Join(exeDir, "probe.toml"),
		filepath.Join(exeDir, "config", "probe.toml"),
		filepath.Join(exeDir, "..", "config", "probe.toml"),
		filepath.Join(exeDir, "..", "..", "config", "probe.toml"),
		filepath.Join(wd, "probe.toml"),
		filepath.Join(wd, "config", "probe.toml"),
	}

	got := probeConfigCandidates(exeDir, wd)
	if len(got) != len(want) {
		t.Fatalf("候选数量不符: want %d, got %d\n%v", len(want), len(got), got)
	}
	for i := range want {
		if filepath.Clean(got[i]) != filepath.Clean(want[i]) {
			t.Errorf("候选 %d 不符: want %s, got %s", i, filepath.Clean(want[i]), filepath.Clean(got[i]))
		}
	}
}

// exe 目录与 cwd 相同时不应产生重复候选。
func TestProbeConfigCandidatesDedup(t *testing.T) {
	dir := filepath.Join("E:", "root", "sub", "same")

	got := probeConfigCandidates(dir, dir)

	seen := map[string]bool{}
	for _, p := range got {
		p = filepath.Clean(p)
		if seen[p] {
			t.Fatalf("候选重复: %s\n%v", p, got)
		}
		seen[p] = true
	}
	// exe 分组 4 项，cwd 分组的 2 项均已出现
	if len(got) != 4 {
		t.Fatalf("去重后候选数量应为 4, got %d\n%v", len(got), got)
	}
}

// 取不到 exe 目录或 cwd 时应跳过对应分组，而不是产出相对当前目录的野路径。
func TestProbeConfigCandidatesEmptyInputs(t *testing.T) {
	if got := probeConfigCandidates("", ""); len(got) != 0 {
		t.Fatalf("两者皆空应返回空候选, got %v", got)
	}
	if got := probeConfigCandidates("", filepath.Join("F:", "wd")); len(got) != 2 {
		t.Fatalf("仅 cwd 应返回 2 条候选, got %v", got)
	}
	if got := probeConfigCandidates(filepath.Join("F:", "root", "exe"), ""); len(got) != 4 {
		t.Fatalf("仅 exe 应返回 4 条候选, got %v", got)
	}
}

// findProbeConfig 在没有任何 exe 旁候选时应回退到 cwd 下的 config/probe.toml。
func TestFindProbeConfigFromWorkingDir(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "probe.toml")
	if err := os.WriteFile(cfgPath, []byte("timeout_ms = 200\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	got, err := findProbeConfig("")
	if err != nil {
		t.Fatalf("findProbeConfig 返回错误: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(cfgPath) {
		t.Fatalf("want %s, got %q", cfgPath, got)
	}
}

// 显式路径不存在时必须报错，而不是静默回退。
func TestFindProbeConfigExplicitMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")

	if _, err := findProbeConfig(missing); err == nil {
		t.Fatal("显式指定不存在的路径应返回错误")
	}
}

func containsPath(list []string, want string) bool {
	for _, p := range list {
		if filepath.Clean(p) == filepath.Clean(want) {
			return true
		}
	}
	return false
}
