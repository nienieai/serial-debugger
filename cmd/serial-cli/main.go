package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"serial-tool-v3/client"
	"serial-tool-v3/config"
	"serial-tool-v3/version"
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
	args := os.Args[1:]
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

	_, err := client.CallOnce("status", nil, "cli")
	if err == nil {
		fmt.Println("IPC 健康检查: ok")
		if !processRunning {
			fmt.Println("(tasklist 检测失效，但守护进程实际运行中)")
		}
	} else {
		if processRunning {
			fmt.Printf("管道连接 / IPC 健康检查: 失败 (%v)\n", err)
		} else {
			fmt.Printf("守护进程未运行 (%v)\n", err)
		}
	}
}

func cmdStart() {
	status, err := client.StartDaemon()
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": "%s"}`+"\n", err.Error())
		os.Exit(1)
	}
	if status == "already_running" {
		fmt.Println("守护进程已在运行中")
	} else {
		fmt.Println("守护进程已启动")
	}
}

func runCommandInteractive(dc *client.DaemonClient, args []string) {
	switch args[0] {
	case "start":
		cmdStart()
		return
	case "status":
		result, err := client.CallOnce("status", nil, "cli")
		if err != nil { fmt.Fprintln(os.Stderr, "错误:", err); return }
		printJSON(result)
		return
	}
	// For other commands, use persistent client
	runCommandWithClient(dc, args, true)
}

func runCommand(args []string) {
	switch args[0] {
	case "start":
		cmdStart()
		return
	default:
		dc, err := client.NewDaemonClient("cli")
		if err != nil {
			fmt.Fprintf(os.Stderr, `{"error": "daemon not running: %s"}`+"\n", err.Error())
			os.Exit(1)
		}
		defer dc.Close()
		runCommandWithClient(dc, args, false)
	}
}

func runCommandWithClient(dc *client.DaemonClient, args []string, interactive bool) {
	switch args[0] {
	case "start":
		cmdStart()
		return

	case "status":
		result, err := client.CallOnce("status", nil, "cli")
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "ports":
		result, err := dc.Call("ports", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "refresh":
		result, err := dc.Call("ports.refresh", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "threads":
		result, err := dc.Call("threads", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "goroutines":
		result, err := dc.Call("goroutines", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "sessions":
		result, err := dc.Call("process.list", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "create":
		port := ""
		baud := 115200
		mode := "single"
		portB := ""
		baudB := 115200
		for i := 1; i < len(args); i++ {
			if args[i] == "--mode" && i+1 < len(args) {
				mode = args[i+1]; i++
			} else if args[i] == "--portB" && i+1 < len(args) {
				portB = args[i+1]; i++
			} else if args[i] == "--baudB" && i+1 < len(args) {
				baudB, _ = strconv.Atoi(args[i+1]); i++
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
		result, err := dc.Call("process.create", params)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "declare":
		if len(args) < 2 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: declare <port> [baud] [--mode forward] [--portB <p>] [--baudB <b>] [--dataBits <d>] [--stopBits <s>] [--parity <p>]"); return }
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
				if i+1 < len(args) { mode = args[i+1]; i++ }
			case "--portB":
				if i+1 < len(args) { portB = args[i+1]; i++ }
			case "--baudB":
				if i+1 < len(args) { baudB, _ = strconv.Atoi(args[i+1]); i++ }
			case "--dataBits":
				if i+1 < len(args) { dataBits, _ = strconv.Atoi(args[i+1]); i++ }
			case "--stopBits":
				if i+1 < len(args) { stopBits = args[i+1]; i++ }
			case "--parity":
				if i+1 < len(args) { parity = args[i+1]; i++ }
			default:
				if port == "" {
					port = args[i]
				} else if baud == 115200 {
					baud, _ = strconv.Atoi(args[i])
				}
			}
		}
		if port == "" {
			if interactive { fmt.Fprintln(os.Stderr, "错误: 必须指定端口"); return }
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
				if interactive { fmt.Fprintln(os.Stderr, "错误: 转发模式需要 --portB"); return }
				os.Exit(1)
			}
			params["portB"] = portB
			params["baudB"] = baudB
		}
		result, err := dc.Call("process.create", params)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "open":
		if len(args) < 2 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: open <port> [baud]"); return }
			os.Exit(1)
		}
		port := args[1]
		baud := 115200
		if len(args) >= 3 {
			baud, _ = strconv.Atoi(args[2])
		}
		result, err := dc.Call("process.create", map[string]any{"port": port, "baud": baud})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "connect":
		if len(args) < 3 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: connect <processId> <port> [baud]"); return }
			os.Exit(1)
		}
		pid := args[1]
		port := args[2]
		baud := 115200
		if len(args) >= 4 {
			baud, _ = strconv.Atoi(args[3])
		}
		result, err := dc.Call("process.connect", map[string]any{
			"processId": pid, "port": port, "baud": baud,
		})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "disconnect":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("process.disconnect", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "close":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("process.destroy", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

		case "switch":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "用法: serial-cli switch <processId> <port> [baud]")
				if interactive { return }
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
			if err := dc.SwitchPort(pid, port, cfg); err != nil { errExit(err, interactive); return }
			fmt.Println(`{"success": true}`)

	case "setmode":
		if len(args) < 3 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: setmode <processId> <single|forward>"); return }
			os.Exit(1)
		}
		pid := args[1]
		mode := args[2]
		if mode != "single" && mode != "forward" {
			if interactive { fmt.Fprintln(os.Stderr, "错误: mode 必须是 single 或 forward"); return }
			os.Exit(1)
		}
		if err := dc.SetMode(pid, mode); err != nil {
			if err != nil { errExit(err, interactive); return }
		}
		fmt.Println(`{"success": true}`)

	case "send":
		if len(args) < 2 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: send <data> [processId] [--hex]"); return }
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
		if err != nil { errExit(err, interactive); return }
		fmt.Println(`{"success": true}`)

		case "forward":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "用法: serial-cli forward <portA> <portB> [baudA] [baudB]")
				if interactive { return }
				os.Exit(1)
			}
			portA := args[1]
			portB := args[2]
			baudA := 115200
			baudB := 115200
			if len(args) >= 4 {
				if v, err := strconv.Atoi(args[3]); err == nil { baudA = v }
			}
			if len(args) >= 5 {
				if v, err := strconv.Atoi(args[4]); err == nil { baudB = v }
			}
			result, err := dc.ForwardCreate(portA, baudA, portB, baudB)
			if err != nil { errExit(err, interactive); return }
			printJSON(result)

	case "autosend":
		if len(args) < 3 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: autosend start <intervalMs> <mode> [pid]"); fmt.Fprintln(os.Stderr, "      autosend stop [pid]"); fmt.Fprintln(os.Stderr, "      autosend status [pid]"); return }
			os.Exit(1)
		}
		sub := args[1]
		switch sub {
		case "start":
			if len(args) < 4 {
				if interactive { fmt.Fprintln(os.Stderr, "用法: autosend start <intervalMs> <mode> [pid]"); return }
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
			if err := dc.AutoSendStart(pid, intervalMs, mode, loop); err != nil { errExit(err, interactive); return }
			fmt.Println(`{"success": true}`)
		case "stop":
			pid := resolveProcessID(dc, args, 2)
			if err := dc.AutoSendStop(pid); err != nil {
				if err != nil { errExit(err, interactive); return }
			}
			fmt.Println(`{"success": true}`)
		case "interval":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "用法: serial-cli autosend interval <ms> [processId]")
				if interactive { return }
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
				if err != nil { errExit(err, interactive); return }
			}
			fmt.Println(`{"success": true}`)
		case "status":
			pid := resolveProcessID(dc, args, 2)
			result, err := dc.AutoSendStatus(pid)
			if err != nil { errExit(err, interactive); return }
			printJSON(result)
		default:
			fmt.Fprintf(os.Stderr, "unknown autosend subcommand: %s\n", sub)
			if interactive { return }
			os.Exit(1)
		}

	case "sendqueue":
		if len(args) < 2 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: sendqueue <file> [pid]"); return }
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
		if fileErr != nil { errExit(fileErr, interactive); return }
		var entries []map[string]any
		if err := json.Unmarshal(fileData, &entries); err != nil {
			errExit(fmt.Errorf("invalid JSON: %w", err), interactive)
			return
		}
		// Write via IPC
		if dc != nil {
			if err := dc.MultistrWrite(pid, entries); err != nil {
				errExit(err, interactive)
				return
			}
		} else {
			_, err := client.CallOnce("multistr.write", map[string]any{"processId": pid, "entries": entries}, "cli")
			if err != nil { errExit(err, interactive); return }
		}
		fmt.Printf(`{"success": true, "entries": %d}`+"\n", len(entries))

	case "multistr":
		if len(args) < 2 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: multistr <save|load|reload|status> [pid]"); return }
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
				if err := dc.MultistrSave(pid); err != nil { errExit(err, interactive); return }
			} else {
				_, err := client.CallOnce("multistr.save", map[string]any{"processId": pid}, "cli")
				if err != nil { errExit(err, interactive); return }
			}
			fmt.Println(`{"success": true}`)
		case "load":
			if dc != nil {
				entries, err := dc.MultistrLoad(pid)
				if err != nil { errExit(err, interactive); return }
				printJSON(map[string]any{"entries": entries})
			} else {
				result, err := client.CallOnce("multistr.load", map[string]any{"processId": pid}, "cli")
				if err != nil { errExit(err, interactive); return }
				printJSON(result)
			}
		case "reload":
			if dc != nil {
				if err := dc.MultistrReload(pid); err != nil { errExit(err, interactive); return }
			} else {
				_, err := client.CallOnce("multistr.reload", map[string]any{"processId": pid}, "cli")
				if err != nil { errExit(err, interactive); return }
			}
			fmt.Println(`{"success": true}`)
		case "status":
			if dc != nil {
				result, err := dc.AutoSendStatus(pid)
				if err != nil { errExit(err, interactive); return }
				printJSON(result)
			} else {
				result, err := client.CallOnce("autosend.status", map[string]any{"processId": pid}, "cli")
				if err != nil { errExit(err, interactive); return }
				printJSON(result)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown multistr subcommand: %s\n", sub)
			if interactive { return }
			os.Exit(1)
		}

	case "probe":
		var ports []string
		configPath := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--config" && i+1 < len(args) {
				configPath = args[i+1]
				i++
			} else {
				ports = append(ports, args[i])
			}
		}
		results, err := dc.ProbePorts(ports, nil, nil, configPath)
		if err != nil { errExit(err, interactive); return }
		if len(results) == 0 {
			fmt.Println("未检测到已知设备")
		} else {
			printJSON(map[string]any{"results": results})
		}

	case "monitor":
		timeout := 0
		if len(args) >= 2 {
			timeout, _ = strconv.Atoi(args[1])
		}
		cmdMonitor(dc, timeout)

	case "shutdown":
		result, err := dc.Call("shutdown", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)
		fmt.Println("daemon shutdown requested")

	case "history":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("session.history", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		translateHistory(result)
		printJSON(result)

	case "stats":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("session.stats", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-files":
		result, err := dc.Call("history.files", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-search":
		if len(args) < 3 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: history-search <file> <keyword> [limit]"); return }
			os.Exit(1)
		}
		filename := args[1]
		keyword := args[2]
		limit := 100
		if len(args) >= 4 {
			limit, _ = strconv.Atoi(args[3])
		}
		result, err := dc.Call("history.search", map[string]any{"file": filename, "keyword": keyword, "limit": limit, "offset": 0})
		if err != nil { errExit(err, interactive); return }
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
		result, err := dc.Call("history.enable", map[string]any{"enabled": enabled})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-status":
		result, err := dc.Call("history.status", nil)
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-attach":
		if len(args) < 3 {
			if interactive { fmt.Fprintln(os.Stderr, "用法: history-attach <processId> <file>"); return }
			os.Exit(1)
		}
		result, err := dc.Call("history.attach", map[string]any{"processId": args[1], "file": args[2]})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-new":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("history.new", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	case "history-detach":
		pid := resolveProcessID(dc, args, 1)
		result, err := dc.Call("history.detach", map[string]any{"processId": pid})
		if err != nil { errExit(err, interactive); return }
		printJSON(result)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "run 'serial-cli help' for available commands")
		if interactive { return }
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
	result, err := dc.Call("process.list", nil)
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

func printHelp() {
	fmt.Print(`用法:
  serial-cli <cmd>              命令行客户端

守护进程:
  start                              启动守护进程（如已运行则返回状态）
  check                              检查守护进程是否运行（进程检测 + IPC 健康检查）
  status                             守护进程状态
  shutdown                           关闭守护进程

端口:
  ports                              可用串口列表（缓存，不刷新）
  refresh                            刷新串口列表
  probe  [ports...] [--config <path>]  设备端口探测（发送探针帧识别设备类型）

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
  history [processId]                历史缓冲区（含毫秒时间戳 + Hex）

自动发送:
  autosend start <ms> <mode> [--loop] [pid]  启动自动发送 (mode: single|queue, --loop循环)
  autosend stop [pid]                        停止自动发送
  autosend status [pid]                      查看自动发送状态
  sendqueue <json-file> [pid]                从JSON文件读取条目数组写入发送队列

多字符串:
  multistr save [pid]                        持久化当前条目到磁盘
  multistr load [pid]                        从磁盘加载条目到发送队列
  multistr reload [pid]                      从发送队列刷新缓存
  multistr status [pid]                      查看多字符串发送状态

	历史记录:
	  history [processId]                内存缓冲区历史
	  history-files                      列出所有历史记录文件 (.log)
	  history-search <file> <kw> [limit] 在历史文件中搜索关键字
	  history-enable [true|false]         开关自动保存 (默认 true)
	  history-status                     查看自动保存状态
	  history-attach <pid> <file>         为进程附加历史文件 (加载内容+继续追加)
	  history-new [pid]                   为进程新建历史文件
	  history-detach [pid]                分离进程的当前历史文件

诊断:
  stats   [processId]                查看进程 I/O 统计（速率/字节/错误）
  sessions                           所有进程列表（含 idle 和 connected）
  monitor [timeout]                   实时监听事件（可选超时秒数）
  threads                            线程/会话详情
  goroutines                         Goroutine 调用栈

其他:
  help                               显示帮助
`)
}
