//go:build linux

package sysperm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// deviceGlobs 限定这两组节点：CH340 / CP210x / FTDI 走 ttyUSB，CDC-ACM 走 ttyACM。
// 刻意不扫 /dev/ttyS*（主板原生串口，权限模型与 USB 设备不同）也不扫 /dev/pts。
var deviceGlobs = []string{"/dev/ttyUSB*", "/dev/ttyACM*"}

const (
	accessRead  = 0x4 // R_OK
	accessWrite = 0x2 // W_OK
)

// Probe 扫一遍现有 USB 串口节点，用 access(2) 判断当前用户能不能读写。
//
// 必须用 access 而不是看 mode 位：uaccess 与 setfacl 下发的是 ACL，
// 文件模式位上看不出来 —— 加过 ACL 的设备仍然是 crw-rw----。
func Probe() Status {
	st := Status{Supported: true, Devices: []string{}, Denied: []string{}}
	for _, g := range deviceGlobs {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		for _, d := range matches {
			st.Devices = append(st.Devices, d)
			if err := syscall.Access(d, accessRead|accessWrite); err != nil {
				st.Denied = append(st.Denied, d)
			}
		}
	}
	return st
}

// authScript 是**编译进二进制**的固定脚本，经 pkexec 以 root 执行。
//
// 安全红线：除了设备路径，不接受任何外部输入，也绝不执行安装目录里的 .sh ——
// 那个目录在 $HOME 下、当前用户可写，pkexec 它等于给本机开了个提权后门。
// 路径还做了白名单校验，防止调用方传进来奇怪的东西。
const authScript = `set -e
uid="$1"; shift
[ "$#" -gt 0 ] || exit 64
for d in "$@"; do
  case "$d" in /dev/ttyUSB*|/dev/ttyACM*) ;; *) exit 64 ;; esac
  if command -v setfacl >/dev/null 2>&1; then
    setfacl -m "u:${uid}:rw" "$d" 2>/dev/null || chmod 0666 "$d"
  else
    chmod 0666 "$d"
  fi
done`

// FixTemp 对当前读不到的节点做一次临时授权。
//
// 优先 setfacl（只给当前用户开洞，不放开给所有人）；系统里没有 setfacl 时退回
// chmod 0666 —— 设备会短暂变成所有人可读写，拔插或重启即恢复。这个取舍写在这里，
// 是因为没有 acl 的机器上除了 0666 没有别的非永久手段。
func FixTemp() FixResult {
	st := Probe()
	if len(st.Devices) == 0 {
		return FixResult{Reason: "no_device"}
	}
	if len(st.Denied) == 0 {
		return FixResult{OK: true}
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return FixResult{Reason: "no_pkexec", Detail: err.Error()}
	}

	args := []string{"/bin/sh", "-c", authScript, "_", strconv.Itoa(os.Getuid())}
	args = append(args, st.Denied...)
	cmd := exec.Command("pkexec", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// 先认「没有授权代理」这种：pkexec 找不到桌面会话里的 polkit 代理时会
			// 退而造一个文本代理，连控制终端都没有就报
			// `Error creating textual authentication agent: ... /dev/tty: No such device
			// or address`（GUI 从 SSH / 非桌面会话启动时必然踩到）。这不是权限问题，
			// 换句话说明白比原样弹英文有用。
			if strings.Contains(strings.ToLower(msg), "authentication agent") {
				return FixResult{Reason: "no_agent", Detail: msg}
			}
			// 126 = 授权没拿到（取消 / 被 polkit 拒绝），127 = 程序找不到。
			// 两者对用户是同一句话：这次没授予权限。
			if ee.ExitCode() == 126 {
				return FixResult{Reason: "cancel", Detail: msg}
			}
			return FixResult{Reason: "failed", Detail: msg}
		}
		return FixResult{Reason: "failed", Detail: err.Error()}
	}

	// 授权窗口说成功不等于真放开：再探一次，只有设备真的可读写才算数。
	if after := Probe(); len(after.Denied) > 0 {
		return FixResult{Reason: "failed", Detail: "still denied: " + strings.Join(after.Denied, ", ")}
	}
	return FixResult{OK: true}
}
