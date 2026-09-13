package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nienieai/serial-debugger/client"
	"github.com/nienieai/serial-debugger/config"
	"github.com/nienieai/serial-debugger/contract"
	"github.com/nienieai/serial-debugger/protocol"
	"github.com/nienieai/serial-debugger/version"
)

var cliLang = "zh"

func detectLang() string {
	// 1. --lang flag
	for i, a := range os.Args[1:] {
		if a == "--lang" && i+1 < len(os.Args[1:]) {
			lang := os.Args[1:][i+1]
			if config.HasLang(lang) {
				return lang
			}
		}
	}
	// 2. LANG env
	if s := os.Getenv("LANG"); s != "" {
		if config.HasLang(s) {
			return s
		}
	}
	// 3. Default
	return "zh"
}

func main() {
	cliLang = detectLang()
	args := stripLangFlag(os.Args[1:])
	if len(args) == 0 {
		interactiveMode()
		return
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp()
	case "check":
		cmdCheck()
	default:
		runCommand(args)
	}
}

// stripLangFlag removes --lang <lang> / --lang=<lang> from the argument list.
// detectLang has already consumed the value, but leaving the flag in place made
// it dispatch as a command ("unknown command: --lang").
func stripLangFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--lang":
			i++ // skip the value that follows
		case strings.HasPrefix(args[i], "--lang="):
			// self-contained form
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func interactiveMode() {
	fmt.Println("串口调试工具 CLI v" + version.Version)
	fmt.Println("输入 help 查看命令，exit 退出")
	fmt.Println()

	dc, err := client.NewDaemonClient("cli")
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法连接守护进程: %v\n", err)
		fmt.Fprintln(os.Stderr, "请先运行 serial-cli start 启动守护进程")
		os.Exit(1)
	}
	defer dc.Close()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			fmt.Println("再见")
			return
		}

		args := parseArgs(line)
		if len(args) == 0 {
			continue
		}

		switch args[0] {
		case "help", "-h", "--help":
			printHelp()
		case "check":
			cmdCheck()
		default:
			runCommandInteractive(dc, args)
		}
	}
}

func parseArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func cmdCheck() {
	processRunning := client.IsDaemonProcessRunning()
	fmt.Printf("进程检测 (tasklist): %v\n", processRunning)

	info, err := client.DaemonInfo()
	if err == nil {
		fmt.Println("IPC 健康检查: ok")
		fmt.Printf("守护进程版本: %s (IPC 协议 %d)\n", info.Version, info.ProtocolVersion)
		fmt.Printf("客户端版本:   %s (IPC 协议 %d)\n", version.Version, protocol.ProtocolVersion)
		if !info.Compatible() {
			fmt.Println("⚠ 协议版本不匹配，请执行 serial-cli start 重启守护进程")
		}
		if !processRunning {
			fmt.Println("(tasklist 检测失效，但守护进程实际运行中)")
		}
		return
	}

	// daemon.info 不可用时区分三种情况，不再一律报「守护进程未运行」。
	if strings.HasPrefix(err.Error(), protocol.UnknownMethodPrefix) {
		fmt.Printf("守护进程版本过旧（不支持 daemon.info），当前客户端为 %s (IPC 协议 %d)\n",
			version.Version, protocol.ProtocolVersion)
		fmt.Println("请执行 serial-cli start 重启守护进程")
		return
	}
	if processRunning {
		fmt.Printf("管道连接 / IPC 健康检查: 失败 (%v)\n", err)
	} else {
		fmt.Printf("守护进程未运行 (%v)\n", err)
	}
}

func cmdStart() {
	status, err := client.StartDaemon()
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", err.Error())
		os.Exit(1)
	}
	switch status {
	case "already_running":
		fmt.Println("守护进程已在运行中")
	case "restarted":
		fmt.Println("检测到守护进程版本不一致，已重启为当前版本")
	default:
		fmt.Println("守护进程已启动")
	}
}

// cmdLogs 打印守护进程日志的末尾若干行。
//
// 守护进程日志固定写在 <exe>/logs/daemon.log（daemon/logfile.go）。发布包里
// serial-daemon.exe 与 serial-cli.exe 同目录，所以这里用自身 exe 目录推导即可，
// 无需连守护进程——守护进程起不来时才是这个命令最有用的时刻。
func cmdLogs(args []string) {
	n := 50
	if len(args) >= 2 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			n = v
		}
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法确定程序目录:", err)
		os.Exit(1)
	}
	path := filepath.Join(filepath.Dir(exe), "logs", "daemon.log")

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法读取守护进程日志 %s: %v\n", path, err)
		fmt.Fprintln(os.Stderr, "提示：守护进程启动后才会创建该文件。")
		os.Exit(1)
	}
	defer f.Close()

	// 只保留最后 n 行，避免一次性把大文件读进内存。
	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(lines) == n {
			copy(lines, lines[1:])
			lines = lines[:n-1]
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "读取日志失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("守护进程日志: %s\n", path)
	if len(lines) == 0 {
		fmt.Println("（暂无内容）")
		return
	}
	for _, l := range lines {
		fmt.Println(l)
	}
}

func runCommandInteractive(dc *client.DaemonClient, args []string) {
	switch args[0] {
	case "start":
		cmdStart()
		return
	case "logs":
		cmdLogs(args)
		return
	case "status":
		result, err := client.CallOnce(contract.Status, nil, "cli")
		if err != nil {
			fmt.Fprintln(os.Stderr, "错误:", err)
			return
		}
		printJSON(result)
		return
	}
	// For other commands, use persistent client
	runCommandWithClient(dc, args, true)
}

func runCommand(args []string) {
	// probe 的参数先单独校验一次：参数写错不该等到连上守护进程才报出来，
	// 没有守护进程时更是根本报不出来（外部报告 §4.2 的 `probe --json` 就撞在这）。
	// 下面 case 里会再解析一次，成本可忽略。
	if args[0] == "probe" {
		if _, _, _, perr := parseProbeArgs(args[1:]); perr != nil {
			fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", perr.Error())
			os.Exit(1)
		}
	}

	switch args[0] {
	case "start":
		cmdStart()
		return
	case "logs":
		// 不连守护进程：守护进程起不来时正是最需要看日志的时候，
		// 若先建连接就会以「连不上」失败，永远读不到原因（TODO #30）。
		cmdLogs(args)
		return
	default:
		dc, err := client.NewDaemonClient("cli")
		if err != nil {
			// 不要在这里断言「daemon not running」：连接失败的原因可能是
			// 守护进程未运行、版本不匹配、管道握手失败等，硬套一个错误
			// 结论会把排查方向带偏。让底层错误自己说话。
			fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", err.Error())
			os.Exit(1)
		}
		defer dc.Close()
		runCommandWithClient(dc, args, false)
	}
}

// queueEntry 是 sendqueue 接受的条目结构，字段与守护进程的 MultistrEntry 一致。
//
// 这里刻意**在 CLI 侧**做严格校验：此前把文件解析成 []map[string]any 再走 IPC，
// 未知字段会连丢两次（CLI 的 map 忽略、守护进程的 struct 再忽略），于是
// {"data":"ONE"} 这种写错字段名的输入会返回 success:true 但装进去的是**空内容**，
// 使用者直到发现发不出东西才知道写错了（TODO 已记录）。
//
// 额外容忍 "data" 作为 "content" 的别名：这是最常被写错的字段名。
type queueEntry struct {
	Enabled bool   `json:"enabled"`
	Hex     bool   `json:"hex"`
	Content string `json:"content"`
	Delay   int    `json:"delay"`
	Note    string `json:"note"`
	// 仅为给出更友好的报错而接受，随后回填到 Content。
	Data string `json:"data"`
}

// parseQueueEntries 严格解析 sendqueue 的 JSON 文件。
//
// 三条硬规则（对应已知的三个静默陷阱）：
//  1. 未知字段直接报错——不能静默丢掉使用者真正想传的字段
//  2. 剔除 UTF-8 BOM——PowerShell 5.1 的 Set-Content -Encoding UTF8 会写 BOM，
//     而 json 解析器会报 `invalid character 'ï'`，属常见跨工具互操作问题
//  3. 拒绝内容为空的条目——空条目发不出任何字节，却一路 success
func parseQueueEntries(raw []byte) ([]queueEntry, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var entries []queueEntry
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf(
			"解析发送队列 JSON 失败: %w\n"+
				"接受的字段: content(必填) / hex / enabled / delay / note\n"+
				"示例: [{\"content\":\"ONE\",\"hex\":false,\"enabled\":true}]", err)
	}
	// 顶层必须是数组；多余的尾随内容也要报错，避免「只解析了前半段」。
	if dec.More() {
		return nil, fmt.Errorf("发送队列 JSON 在数组之后还有多余内容")
	}

	for i := range entries {
		if entries[i].Content == "" && entries[i].Data != "" {
			entries[i].Content = entries[i].Data
		}
		if entries[i].Content == "" {
			return nil, fmt.Errorf(
				"第 %d 条的 content 为空。空条目发不出任何字节，"+
					"请检查字段名是否写成了 data/text/value 等", i+1)
		}
	}
	return entries, nil
}

func runCommandWithClient(dc *client.DaemonClient, args []string, interactive bool) {
	switch args[0] {
	case "start":
		cmdStart()
		return

	case "logs":
		cmdLogs(args)
		return

	case "status":
		result, err := client.CallOnce(contract.Status, nil, "cli")
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "ports":
		result, err := dc.Call(contract.Ports, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "refresh":
		result, err := dc.Call(contract.PortsRefresh, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "threads":
		result, err := dc.Call(contract.Threads, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "goroutines":
		result, err := dc.Call(contract.Goroutines, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "sessions":
		result, err := dc.Call(contract.ProcessList, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "create":
		port := ""
		baud := 115200
		mode := "single"
		portB := ""
		baudB := 115200
		for i := 1; i < len(args); i++ {
			if args[i] == "--mode" && i+1 < len(args) {
				mode = args[i+1]
				i++
			} else if args[i] == "--portB" && i+1 < len(args) {
				portB = args[i+1]
				i++
			} else if args[i] == "--baudB" && i+1 < len(args) {
				baudB, _ = strconv.Atoi(args[i+1])
				i++
			} else if port == "" {
				port = args[i]
			} else if baud == 115200 {
				baud, _ = strconv.Atoi(args[i])
			}
		}
		params := map[string]any{"mode": mode}
		if port != "" {
			params["port"] = port
			params["baud"] = baud
		}
		if mode == "forward" && portB != "" {
			params["portB"] = portB
			params["baudB"] = baudB
		}
		result, err := dc.Call(contract.ProcessCreate, params)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "declare":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: declare <port> [baud] [--mode forward] [--portB <p>] [--baudB <b>] [--dataBits <d>] [--stopBits <s>] [--parity <p>]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		port := ""
		baud := 115200
		mode := "single"
		portB := ""
		baudB := 115200
		dataBits := 8
		stopBits := "1"
		parity := "none"
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--mode":
				if i+1 < len(args) {
					mode = args[i+1]
					i++
				}
			case "--portB":
				if i+1 < len(args) {
					portB = args[i+1]
					i++
				}
			case "--baudB":
				if i+1 < len(args) {
					baudB, _ = strconv.Atoi(args[i+1])
					i++
				}
			case "--dataBits":
				if i+1 < len(args) {
					dataBits, _ = strconv.Atoi(args[i+1])
					i++
				}
			case "--stopBits":
				if i+1 < len(args) {
					stopBits = args[i+1]
					i++
				}
			case "--parity":
				if i+1 < len(args) {
					parity = args[i+1]
					i++
				}
			default:
				if port == "" {
					port = args[i]
				} else if baud == 115200 {
					baud, _ = strconv.Atoi(args[i])
				}
			}
		}
		if port == "" {
			fmt.Fprintln(os.Stderr, "错误: 必须指定端口")
			if interactive {
				return
			}
			os.Exit(1)
		}
		params := map[string]any{
			"mode":     mode,
			"port":     port,
			"baud":     baud,
			"dataBits": dataBits,
			"stopBits": stopBits,
			"parity":   parity,
			"connect":  false,
		}
		if mode == "forward" {
			if portB == "" {
				fmt.Fprintln(os.Stderr, "错误: 转发模式需要 --portB")
				if interactive {
					return
				}
				os.Exit(1)
			}
			params["portB"] = portB
			params["baudB"] = baudB
		}
		result, err := dc.Call(contract.ProcessCreate, params)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "open":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: open <port> [baud]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		port := args[1]
		baud := 115200
		if len(args) >= 3 {
			baud, _ = strconv.Atoi(args[2])
		}
		result, err := dc.Call(contract.ProcessCreate, map[string]any{"port": port, "baud": baud})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "connect":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: connect <processId> <port> [baud]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		pid := args[1]
		port := args[2]
		baud := 115200
		if len(args) >= 4 {
			baud, _ = strconv.Atoi(args[3])
		}
		result, err := dc.Call(contract.ProcessConnect, map[string]any{
			"processId": pid, "port": port, "baud": baud,
		})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "disconnect":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call(contract.ProcessDisconnect, map[string]any{"processId": pid})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "close":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call(contract.ProcessDestroy, map[string]any{"processId": pid})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "switch":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: serial-cli switch <processId> <port> [baud]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		pid := args[1]
		port := args[2]
		baud := 115200
		if len(args) >= 4 {
			if v, err := strconv.Atoi(args[3]); err == nil {
				baud = v
			}
		}
		cfg := map[string]any{"baud": baud}
		if err := dc.SwitchPort(pid, port, cfg); err != nil {
			errExit(err, interactive)
			return
		}
		fmt.Println(`{"success": true}`)

	case "setmode":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: setmode <processId> <single|forward>")
			if interactive {
				return
			}
			os.Exit(1)
		}
		pid := args[1]
		mode := args[2]
		if mode != "single" && mode != "forward" {
			fmt.Fprintln(os.Stderr, "错误: mode 必须是 single 或 forward")
			if interactive {
				return
			}
			os.Exit(1)
		}
		if err := dc.SetMode(pid, mode); err != nil {
			if err != nil {
				errExit(err, interactive)
				return
			}
		}
		fmt.Println(`{"success": true}`)

	case "send":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: send <data> [processId] [--hex]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		data := args[1]
		format := "text"
		pid := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--hex" || args[i] == "-x" {
				format = "hex"
			} else {
				pid = args[i]
			}
		}
		if format == "hex" {
			data = strings.ReplaceAll(data, " ", "")
		}
		if pid == "" {
			pid = firstConnectedIDDC(dc)
		}
		err := dc.SendViaShm(pid, data, format)
		if err != nil {
			errExit(err, interactive)
			return
		}
		fmt.Println(`{"success": true}`)

	case "forward":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: serial-cli forward <portA> <portB> [baudA] [baudB]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		portA := args[1]
		portB := args[2]
		baudA := 115200
		baudB := 115200
		if len(args) >= 4 {
			if v, err := strconv.Atoi(args[3]); err == nil {
				baudA = v
			}
		}
		if len(args) >= 5 {
			if v, err := strconv.Atoi(args[4]); err == nil {
				baudB = v
			}
		}
		result, err := dc.ForwardCreate(portA, baudA, portB, baudB)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "autosend":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: autosend start <intervalMs> <mode> [pid]")
			fmt.Fprintln(os.Stderr, "      autosend stop [pid]")
			fmt.Fprintln(os.Stderr, "      autosend status [pid]")
			fmt.Fprintln(os.Stderr, "      autosend interval <ms> [pid]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		sub := args[1]
		switch sub {
		case "start":
			if len(args) < 4 {
				fmt.Fprintln(os.Stderr, "用法: autosend start <intervalMs> <mode> [pid]")
				if interactive {
					return
				}
				os.Exit(1)
			}
			intervalMs, _ := strconv.Atoi(args[2])
			mode := args[3]
			loop := false
			pid := ""
			for i := 4; i < len(args); i++ {
				if args[i] == "--loop" {
					loop = true
				} else {
					pid = args[i]
				}
			}
			if pid == "" {
				pid = firstConnectedIDDC(dc)
			}
			if err := dc.AutoSendStart(pid, intervalMs, mode, loop); err != nil {
				errExit(err, interactive)
				return
			}
			fmt.Println(`{"success": true}`)
		case "stop":
			pid := resolveProcessID(dc, args, 2)
			if err := dc.AutoSendStop(pid); err != nil {
				errExit(err, interactive)
				return
			}
			fmt.Println(`{"success": true}`)
		case "interval":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "用法: serial-cli autosend interval <ms> [processId]")
				if interactive {
					return
				}
				os.Exit(1)
			}
			intervalMs, _ := strconv.Atoi(args[2])
			pid := ""
			if len(args) >= 4 {
				pid = args[3]
			}
			if pid == "" {
				pid = firstConnectedIDDC(dc)
			}
			if err := dc.AutoSendSetInterval(pid, intervalMs); err != nil {
				errExit(err, interactive)
				return
			}
			fmt.Println(`{"success": true}`)
		case "status":
			pid := resolveProcessID(dc, args, 2)
			result, err := dc.AutoSendStatus(pid)
			if err != nil {
				errExit(err, interactive)
				return
			}
			printJSON(result)
		default:
			fmt.Fprintf(os.Stderr, "unknown autosend subcommand: %s\n", sub)
			if interactive {
				return
			}
			os.Exit(1)
		}

	case "sendqueue":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: sendqueue <file> [pid]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		pid := ""
		filePath := args[1]
		if len(args) >= 3 {
			pid = args[2]
		}
		if pid == "" {
			pid = firstConnectedIDDC(dc)
		}
		// Read JSON entries file
		var fileData []byte
		var fileErr error
		if filePath == "-" {
			fileData, fileErr = io.ReadAll(os.Stdin)
		} else {
			fileData, fileErr = os.ReadFile(filePath)
		}
		if fileErr != nil {
			errExit(fileErr, interactive)
			return
		}
		entries, err := parseQueueEntries(fileData)
		if err != nil {
			errExit(err, interactive)
			return
		}
		// Write via IPC
		if dc != nil {
			if err := dc.MultistrWrite(pid, entries); err != nil {
				errExit(err, interactive)
				return
			}
		} else {
			_, err := client.CallOnce(contract.MultistrWrite, map[string]any{"processId": pid, "entries": entries}, "cli")
			if err != nil {
				errExit(err, interactive)
				return
			}
		}
		fmt.Printf(`{"success": true, "entries": %d}`+"\n", len(entries))

	case "multistr":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: multistr <save|load|reload|status> [pid]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		sub := args[1]
		pid := ""
		if len(args) >= 3 {
			pid = args[2]
		}
		if pid == "" {
			pid = firstConnectedIDDC(dc)
		}
		switch sub {
		case "save":
			if dc != nil {
				if err := dc.MultistrSave(pid); err != nil {
					errExit(err, interactive)
					return
				}
			} else {
				_, err := client.CallOnce(contract.MultistrSave, map[string]any{"processId": pid}, "cli")
				if err != nil {
					errExit(err, interactive)
					return
				}
			}
			fmt.Println(`{"success": true}`)
		case "load":
			if dc != nil {
				entries, err := dc.MultistrLoad(pid)
				if err != nil {
					errExit(err, interactive)
					return
				}
				printJSON(map[string]any{"entries": entries})
			} else {
				result, err := client.CallOnce(contract.MultistrLoad, map[string]any{"processId": pid}, "cli")
				if err != nil {
					errExit(err, interactive)
					return
				}
				printJSON(result)
			}
		case "reload":
			if dc != nil {
				if err := dc.MultistrReload(pid); err != nil {
					errExit(err, interactive)
					return
				}
			} else {
				_, err := client.CallOnce(contract.MultistrReload, map[string]any{"processId": pid}, "cli")
				if err != nil {
					errExit(err, interactive)
					return
				}
			}
			fmt.Println(`{"success": true}`)
		case "status":
			if dc != nil {
				result, err := dc.AutoSendStatus(pid)
				if err != nil {
					errExit(err, interactive)
					return
				}
				printJSON(result)
			} else {
				result, err := client.CallOnce(contract.AutosendStatus, map[string]any{"processId": pid}, "cli")
				if err != nil {
					errExit(err, interactive)
					return
				}
				printJSON(result)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown multistr subcommand: %s\n", sub)
			if interactive {
				return
			}
			os.Exit(1)
		}

	case "probe":
		ports, configPath, budgetMs, perr := parseProbeArgs(args[1:])
		if perr != nil {
			errExit(perr, interactive)
			return
		}
		outcome, err := dc.ProbePorts(ports, nil, nil, configPath, budgetMs)
		if err != nil {
			errExit(err, interactive)
			return
		}
		// 「空结果」有三种截然不同的含义，必须让使用者分得清：
		//   1. 探测过但没有规则命中        → 未检测到已知设备
		//   2. 端口被跳过（占用/打不开/…） → 明确列出原因
		//   3. 超出总预算没轮到            → 明确说明还有哪些没试
		if len(outcome.Results) == 0 && len(outcome.Skipped) == 0 {
			fmt.Println("未检测到已知设备")
			break
		}
		printJSON(probeOutcomeJSON(outcome))
		if len(outcome.Results) == 0 {
			fmt.Fprintln(os.Stderr, "注意：以上端口本轮没有被真正探测完，不能据此判断「没有设备」。")
		}

	case "monitor":
		timeout := 0
		if len(args) >= 2 {
			timeout, _ = strconv.Atoi(args[1])
		}
		cmdMonitor(dc, timeout)

	case "shutdown":
		result, err := dc.Call(contract.Shutdown, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)
		fmt.Println("daemon shutdown requested")

	case "history":
		hp, perr := parseHistoryArgs(args[1:])
		if perr != nil {
			errExit(perr, interactive)
			return
		}
		params := map[string]any{"processId": resolveProcessID(dc, hp.positional, 0)}
		if hp.limit > 0 {
			params["limit"] = hp.limit
		}
		if hp.beforeMs > 0 {
			params["beforeTsMs"] = hp.beforeMs
		}
		if hp.skip > 0 {
			params["sameTsSkip"] = hp.skip
		}
		result, err := dc.Call(contract.SessionHistory, params)
		if err != nil {
			errExit(err, interactive)
			return
		}
		translateHistory(result)
		printJSON(result)

	case "stats":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call(contract.SessionStats, map[string]any{"processId": pid})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-files":
		result, err := dc.Call(contract.HistoryFiles, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-search":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: history-search <file> <keyword> [limit]")
			if interactive {
				return
			}
			os.Exit(1)
		}
		filename := args[1]
		keyword := args[2]
		limit := 100
		if len(args) >= 4 {
			limit, _ = strconv.Atoi(args[3])
		}
		result, err := dc.Call(contract.HistorySearch, map[string]any{"file": filename, "keyword": keyword, "limit": limit, "offset": 0})
		if err != nil {
			errExit(err, interactive)
			return
		}
		// Translate system messages in results
		if results, ok := result["results"].([]any); ok {
			for i, r := range results {
				if entry, ok := r.(map[string]any); ok {
					if dir, _ := entry["direction"].(string); dir == "system" {
						if hex, _ := entry["hex"].(string); hex != "" {
							if t := config.FormatSysMsg(cliLang, hex); t != hex {
								entry["hex"] = t
								results[i] = entry
							}
						}
					}
				}
			}
		}
		printJSON(result)

	case "history-enable":
		enabled := true
		if len(args) >= 2 {
			enabled, _ = strconv.ParseBool(args[1])
		}
		result, err := dc.Call(contract.HistoryEnable, map[string]any{"enabled": enabled})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-status":
		result, err := dc.Call(contract.HistoryStatus, nil)
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-attach":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "用法: history-attach <processId> <file>")
			if interactive {
				return
			}
			os.Exit(1)
		}
		result, err := dc.Call(contract.HistoryAttach, map[string]any{"processId": args[1], "file": args[2]})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-new":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call(contract.HistoryNew, map[string]any{"processId": pid})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	case "history-detach":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call(contract.HistoryDetach, map[string]any{"processId": pid})
		if err != nil {
			errExit(err, interactive)
			return
		}
		printJSON(result)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "run 'serial-cli help' for available commands")
		if interactive {
			return
		}
		os.Exit(1)
	}
}

func cmdMonitor(dc *client.DaemonClient, timeout int) {
	fmt.Println("[monitor] connected, waiting for events...")

	var timer *time.Timer
	var timerCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(time.Duration(timeout) * time.Second)
		timerCh = timer.C
	}
	events := dc.ReadEvent()
	for {
		select {
		case <-timerCh:
			return
		case msg, ok := <-events:
			if !ok {
				fmt.Fprintln(os.Stderr, "connection lost")
				os.Exit(1)
			}
			if timer != nil {
				timer.Reset(time.Duration(timeout) * time.Second)
			}
			b, _ := json.Marshal(msg.Params)
			fmt.Printf("[%s] %s\n", msg.Event, string(b))
		}
	}
}

func resolveProcessID(dc *client.DaemonClient, args []string, pos int) string {
	if len(args) >= pos+1 {
		return args[pos]
	}
	return firstConnectedIDDC(dc)
}

func firstConnectedIDDC(dc *client.DaemonClient) string {
	result, err := dc.Call(contract.ProcessList, nil)
	exitOnErr(err)
	processes, _ := result["processes"].([]any)
	for _, p := range processes {
		pmap, _ := p.(map[string]any)
		if status, _ := pmap["status"].(string); status == "connected" {
			id, _ := pmap["processId"].(string)
			return id
		}
	}
	fmt.Fprintln(os.Stderr, `{"error": "no connected process"}`)
	os.Exit(1)
	return ""
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// translateHistory walks a session.history result and translates sys.* entries in-place.
func translateHistory(result map[string]any) {
	entries, _ := result["history"].([]any)
	if entries == nil {
		return
	}
	for i, e := range entries {
		entry, _ := e.(map[string]any)
		if entry == nil {
			continue
		}
		dir, _ := entry["direction"].(string)
		if dir != "system" {
			continue
		}
		hex, _ := entry["hex"].(string)
		if translated := config.FormatSysMsg(cliLang, hex); translated != hex {
			entry["hex"] = translated
			entries[i] = entry
		}
	}
}

func errExit(err error, interactive bool) {
	if !interactive {
		exitOnErr(err)
	}
	fmt.Fprintf(os.Stderr, "错误: %v\n", err)
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", err.Error())
		os.Exit(1)
	}
}

// probeOutcomeJSON 是一次探测结果对外的 JSON 形状。
//
// 抽成独立函数是为了能在测试里把字段集合锁住：`操作说明.md` 明确承诺了 `busy`
// （并建议用 `grep busy` 自检），而 CLI 曾经漏掉这个键 —— 守护进程返回了它，
// 序列化时被丢在门外，于是文档写的自检方法永远匹配不到。外部测试报告点名过这一条。
func probeOutcomeJSON(o *client.ProbeOutcome) map[string]any {
	if o == nil {
		return map[string]any{}
	}
	return map[string]any{
		"results":         o.Results,
		"skipped":         o.Skipped,
		"attempts":        o.Attempts,
		"elapsedMs":       o.ElapsedMs,
		"budgetExhausted": o.BudgetExhausted,
		"busy":            o.Busy,
	}
}

// parseProbeArgs 解析 `probe` 的参数。
//
// 未知的长选项**必须报错**：此前 `--json` 这类不存在的 flag 会被当成端口名，
// 于是结果里多出一条「端口 --json 打不开」的 skipped，读者会真的以为有个端口有问题
// （外部测试报告 0.7.5.3 轮 §4.2）。缺值的 `--config` / `--budget` 也一并报清楚。
func parseProbeArgs(args []string) (ports []string, configPath string, budgetMs int, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config":
			if i+1 >= len(args) {
				return nil, "", 0, fmt.Errorf("--config 需要一个配置文件路径")
			}
			configPath = args[i+1]
			i++
		case args[i] == "--budget":
			if i+1 >= len(args) {
				return nil, "", 0, fmt.Errorf("--budget 需要一个毫秒数")
			}
			n, aerr := strconv.Atoi(args[i+1])
			if aerr != nil || n <= 0 {
				return nil, "", 0, fmt.Errorf("--budget 需要正整数毫秒数，实际为 %q", args[i+1])
			}
			budgetMs = n
			i++
		case len(args[i]) > 1 && strings.HasPrefix(args[i], "-"):
			return nil, "", 0, fmt.Errorf("未知参数 %q（probe 支持：端口名…、--config <路径>、--budget <毫秒>）", args[i])
		default:
			ports = append(ports, args[i])
		}
	}
	return ports, configPath, budgetMs, nil
}

// historyArgs 是 `history` 命令解析后的参数。
type historyArgs struct {
	positional []string // 进程号（可选）
	limit      int
	beforeMs   int64
	skip       int
}

// parseHistoryArgs 解析 `history` 的参数。
//
// 规矩与 parseProbeArgs 一致：未知的长选项必须报错，不能当成进程号 —— 否则
// `history --json` 会变成「查进程 --json 的历史」这种看不懂的错误。
func parseHistoryArgs(args []string) (historyArgs, error) {
	var out historyArgs
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--limit":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--limit 需要一个条数")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return out, fmt.Errorf("--limit 需要正整数条数，实际为 %q", args[i+1])
			}
			out.limit = n
			i++
		case args[i] == "--before":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--before 需要一个毫秒时间戳")
			}
			n, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil || n <= 0 {
				return out, fmt.Errorf("--before 需要正整数毫秒时间戳（取上一条响应里的 tsMs），实际为 %q", args[i+1])
			}
			out.beforeMs = n
			i++
		case args[i] == "--skip":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--skip 需要一个条数")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				return out, fmt.Errorf("--skip 需要非负整数条数，实际为 %q", args[i+1])
			}
			out.skip = n
			i++
		case len(args[i]) > 1 && strings.HasPrefix(args[i], "-"):
			return out, fmt.Errorf("未知参数 %q（history 支持：进程号、--limit <条数>、--before <毫秒>、--skip <条数>）", args[i])
		default:
			out.positional = append(out.positional, args[i])
		}
	}
	return out, nil
}

func printHelp() {
	fmt.Print(`用法:
  serial-cli <cmd>              命令行客户端

守护进程:
  start                              启动守护进程（如已运行则返回状态）
  check                              检查守护进程状态与版本一致性（进程检测 + IPC + 协议版本）
  status                             守护进程状态
  shutdown                           关闭守护进程

端口:
  ports                              可用串口列表（缓存，不刷新）
  refresh                            刷新串口列表
  probe  [ports...] [--config <path>] [--budget <ms>]  设备端口探测（发送探针帧识别设备类型）

进程:
  create  [port] [baud] [--mode forward] [--portB <portB>]  创建进程（默认立即连接）
  declare <port> [baud] [--mode forward] [--portB <p>]    声明端口配置（不打开串口，全客户端可见）
  open    <port> [baud]              打开串口（等同于 create <port> [baud]）
  connect <processId> <port> [baud]  将空闲进程连接到串口
  disconnect [processId]             断开串口连接（进程保留为空闲）
  close   [processId]                销毁进程（有连接则先断开）
  switch  <processId> <port> [baud]  切换进程到不同端口（保留进程历史）
  setmode <processId> <single|forward> 切换进程模式（需 idle 状态）
  forward <portA> <portB> [baudA] [baudB] 创建端口转发

数据:
  send    <data> [processId] [--hex] 发送数据 (--hex: 十六进制)
  history [processId] [--limit <n>] [--before <ms>] [--skip <n>]
                                     历史缓冲区（含毫秒时间戳 + Hex）
                                     --limit 只取最新 n 条；--before/--skip 用上一条
                                     响应里最旧一条的 tsMs 与同毫秒条数继续往前翻

自动发送:
  autosend start <ms> <mode> [--loop] [pid]  启动自动发送 (mode: single|queue, --loop循环)
  autosend stop [pid]                        停止自动发送
  autosend status [pid]                      查看自动发送状态
  autosend interval <ms> [pid]               修改自动发送间隔
  sendqueue <json-file> [pid]                从JSON文件读取条目数组写入发送队列

多字符串:
  multistr save [pid]                        持久化当前条目到磁盘
  multistr load [pid]                        从磁盘加载条目到发送队列
  multistr reload [pid]                      从发送队列刷新缓存
  multistr status [pid]                      查看多字符串发送状态

历史记录:
  history-files                      列出所有历史记录文件 (.log)
  history-search <file> <kw> [limit] 在历史文件中搜索关键字
  history-enable [true|false]        开关自动保存 (默认 true)
  history-status                     查看自动保存状态
  history-attach <pid> <file>        为进程附加历史文件 (加载内容+继续追加)
  history-new [pid]                  为进程新建历史文件
  history-detach [pid]               分离进程的当前历史文件

诊断:
  stats   [processId]                查看进程 I/O 统计（速率/字节/错误）
  sessions                           所有进程列表（含 idle 和 connected）
  monitor [timeout]                   实时监听事件（可选超时秒数）
  threads                            线程/会话详情
  goroutines                         Goroutine 调用栈
  logs [n]                           打印守护进程日志末尾 n 行 (默认 50)
                                     日志文件: <exe>/logs/daemon.log
                                     无需守护进程在运行即可查看

其他:
  help                               显示帮助
`)
}
