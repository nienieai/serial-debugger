package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// 同一份 exe（大小写、`.`/`..` 归一化之后相同）必须落到同一个实例标识上，
// 否则「从快捷方式启动」和「从命令行启动」会各起一个 GUI。
func TestGuiInstanceIDStableForSamePath(t *testing.T) {
	base := filepath.Join(`C:\Embedded\test_py`, "串口调试工具", "0.9.0-dev", "serial-gui-dev.exe")
	id := instanceIDFor(base)
	cases := []string{
		base,
		strings.ToUpper(base),
		strings.ToLower(base),
		filepath.Join(`C:\Embedded\test_py`, "串口调试工具", "0.9.0-dev", ".", "serial-gui-dev.exe"),
		filepath.Join(`C:\Embedded\test_py`, "串口调试工具", "0.9.0-dev", "logs", "..", "serial-gui-dev.exe"),
	}
	for _, c := range cases {
		if got := instanceIDFor(c); got != id {
			t.Errorf("instanceIDFor(%q) = %q, 期望与 %q 相同", c, got, id)
		}
	}
}

// 不同路径（开发版 / 部署版 / 另一台目录）必须是不同实例标识。
func TestGuiInstanceIDDiffersByPath(t *testing.T) {
	paths := []string{
		`C:\Embedded\test_py\serial-tool\build\bin\serial-gui.exe`,
		`C:\Embedded\test_py\串口调试工具\0.9.0-dev\serial-gui-dev.exe`,
		`C:\Embedded\test_py\串口调试工具\0.8.4\serial-gui-dev.exe`,
	}
	seen := make(map[string]string, len(paths))
	for _, p := range paths {
		id := instanceIDFor(p)
		if prev, ok := seen[id]; ok {
			t.Errorf("路径 %q 与 %q 落到了同一个实例标识 %q", p, prev, id)
		}
		seen[id] = p
	}
}

// 空路径退化成全局唯一标识：不 panic，也不返回空串（空 UniqueID 会让 v3 的单实例
// 机制直接报错，整个 GUI 起不来）。
func TestGuiInstanceIDEmptyPath(t *testing.T) {
	for _, p := range []string{"", "   "} {
		if got := instanceIDFor(p); got != guiInstanceBaseID {
			t.Errorf("instanceIDFor(%q) = %q, 期望退化标识 %q", p, got, guiInstanceBaseID)
		}
	}
}
