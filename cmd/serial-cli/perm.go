package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nienieai/serial-debugger/sysperm"
)

// cmdFixPermissions 是 `serial-cli fix-permissions`：对当前读不到写不了的 USB 串口
// 做一次**临时授权**（pkexec 弹一次密码框），不写任何系统文件。
//
// 刻意只做临时授权：解压即用的版本不该替用户改系统。永久修复是安装时的决定 ——
// build/linux/install.sh --fix-permissions 会往 /etc/udev/rules.d 写一条 uaccess
// 规则，一次授权后不再打扰。代价是拔插或重启后设备节点重建、ACL 消失，这条命令
// 需要再跑一次。
//
// 不依赖守护进程：权限是设备节点的事，跟守护进程起没起没有关系。
func cmdFixPermissions(args []string) {
	assumeYes := false
	asJSON := false
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			assumeYes = true
		case "--json":
			asJSON = true
		case "help", "-h", "--help":
			fmt.Println(`用法: serial-cli fix-permissions [--yes] [--json]
  对当前不可读写的 USB 串口（/dev/ttyUSB*、/dev/ttyACM*）做一次临时授权。
  --yes   不问，直接弹授权窗口
  --json  以 JSON 输出结果（不询问）`)
			return
		default:
			printErr("未知参数: %s（可用 --yes / --json）", a)
			os.Exit(2)
		}
	}

	st := sysperm.Probe()

	// --json 是给脚本用的：一条路径走到底，不打印人读的行，也不问任何问题。
	if asJSON {
		res := sysperm.FixResult{}
		code := 0
		switch {
		case !st.Supported:
			res = sysperm.FixResult{Reason: "unsupported", Detail: "linux only"}
			code = 1
		case len(st.Devices) == 0:
			res = sysperm.FixResult{Reason: "no_device"}
		case !st.NeedsFix():
			res = sysperm.FixResult{OK: true}
		default:
			res = sysperm.FixTemp()
			if !res.OK {
				code = 1
			}
		}
		printJSON(struct {
			sysperm.Status
			OK     bool   `json:"ok"`
			Reason string `json:"reason"`
			Detail string `json:"detail"`
		}{st, res.OK, res.Reason, res.Detail})
		os.Exit(code)
	}

	if !st.Supported {
		printErr("临时授权只支持 Linux。其它平台的串口设备本来就不需要提权。")
		os.Exit(1)
	}
	if len(st.Devices) == 0 {
		fmt.Println("没有检测到 USB 串口设备（/dev/ttyUSB* 或 /dev/ttyACM*）。")
		return
	}
	if !st.NeedsFix() {
		fmt.Printf("当前用户已经能读写：%s，无需授权。\n", strings.Join(st.Devices, ", "))
		return
	}

	fmt.Println("串口权限")
	fmt.Printf("  设备: %s\n", strings.Join(st.Devices, ", "))
	fmt.Printf("  需要授权: %s\n", strings.Join(st.Denied, ", "))
	fmt.Println("  授权会弹出一次系统密码框，只给当前用户加读写权限；")
	fmt.Println("  拔插或重启后失效，需要再执行一次本命令。")

	if !assumeYes {
		// 非交互环境绝不阻塞等输入：CI / 脚本里没人会敲 y，等下去就是卡死。
		if !isTerminal(os.Stdin) {
			printWarn("非交互环境，未授权。加 --yes 可直接执行。")
			os.Exit(1)
		}
		fmt.Print("要现在授权吗？[y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
		default:
			fmt.Println("已取消。")
			os.Exit(1)
		}
	}

	res := sysperm.FixTemp()
	if res.OK {
		fmt.Printf("已授权：%s 现在可读写。\n", strings.Join(st.Denied, ", "))
		return
	}
	switch res.Reason {
	case "cancel":
		printErr("没有获得授权（密码框被取消或被拒绝）。")
	case "no_pkexec":
		printErr("没有找到 pkexec，弹不出授权窗口。可请管理员手工执行：")
		printErr("  sudo chmod a+rw %s", strings.Join(st.Denied, " "))
	case "no_device":
		printErr("设备在授权前消失了，请重新插拔后再试。")
	default:
		printErr("授权失败: %s", res.Detail)
	}
	os.Exit(1)
}
