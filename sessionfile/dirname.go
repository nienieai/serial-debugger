package sessionfile

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxDirNameRunes 是目录名的长度上限（按 rune 计），与线程名的 64 字符一致。
//
// 不取 Windows 的 255：整个路径还有 MAX_PATH 260 的限制，工程目录通常埋在
// 好几层之下，留出余量比把名字写到极限重要。
const MaxDirNameRunes = 64

// windowsBadRunes 是 Windows 文件名里不允许出现的字符。
//
// 这些字符在 Linux 上合法，但工程目录会被两个平台共用（vm 挂共享盘、WSL 访问
// /mnt/c 都会碰到），按最严的那个平台收敛，免得同一个工程换个系统就打不开。
const windowsBadRunes = `<>:"/\|?*`

// SanitizeDirName 把用户起的线程名收敛成一个能当目录名的字符串。
//
// 非法字符是**删掉**而不是替换成下划线：`a/b` 和 `a_b` 替换后会撞成同一个名字，
// 而它们本来就是两个不同的会话。删掉之后至少还分得开。
//
// 返回空串表示这个名字没法当目录名（比如全是非法字符），调用方应保持原目录名。
func SanitizeDirName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7F {
			continue // 控制字符
		}
		if strings.ContainsRune(windowsBadRunes, r) {
			continue
		}
		b.WriteRune(r)
	}
	// 连续空白压成一个；Windows 也不接受首尾的空格与点
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, " .")
	if out == "" {
		return ""
	}
	if utf8.RuneCountInString(out) > MaxDirNameRunes {
		rs := []rune(out)
		out = strings.Trim(string(rs[:MaxDirNameRunes]), " .")
		if out == "" {
			return ""
		}
	}
	if isWindowsReservedName(out) {
		out += "_"
	}
	return out
}

// isWindowsReservedName 判断是不是 Windows 的保留设备名。
//
// CON / NUL / COM1 这类名字即使在 NTFS 上也会被当成设备，建同名目录会失败或
// 指到一个根本不存在的地方。带扩展名的同样算（COM1.txt 也是设备）。
func isWindowsReservedName(name string) bool {
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

// SessionDirNameFor 按会话身份重算「自动名」目录名（时间_uuid8）。
//
// 用户把名字清空、退回自动名时用它：目录名不靠记着什么原始值，而是从
// manifest 里的 created + sessionId 重新算出来，怎么都还原得回去。
func SessionDirNameFor(created, sessionID string) string {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		t = time.Now()
	}
	return SessionDirName(t, sessionID)
}

// UniqueDirName 在 parent 下找一个没被占用的目录名：base、base (2)、base (3)…
//
// 只做「看得见的存在性检查」，不抢锁：两个同名工程在同一瞬间改名仍然可能撞在
// 一起，这时由随后的 rename 自己失败（目标已存在），调用方按失败降级处理。
func UniqueDirName(parent, base string) string {
	if cand := filepath.Join(parent, base); !dirExists(cand) {
		return cand
	}
	for i := 2; i <= 999; i++ {
		cand := filepath.Join(parent, base+" ("+strconv.Itoa(i)+")")
		if !dirExists(cand) {
			return cand
		}
	}
	// 999 个同名的都占满了：挂一个时间戳保证不覆盖别人的目录
	return filepath.Join(parent, base+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
