package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nienieai/serial-debugger/config"
)

// ---- probe config (TOML) ----

type ProbeConfig struct {
	TimeoutMs int         `toml:"timeout_ms"`
	BaudRates []int       `toml:"baud_rates"`
	Rules     []ProbeRule `toml:"rules"`
}

type ProbeRule struct {
	Name           string `toml:"name"`
	PortPattern    string `toml:"port_pattern"`
	ProbeHex       string `toml:"probe_hex"`
	MatchType      string `toml:"match_type"` // "substring", "regex", "modbus_crc"
	MatchValue     string `toml:"match_value"`
	MinResponseLen int    `toml:"min_response_len"`
}

// ---- probe result ----

type ProbeResult struct {
	Port        string `json:"port"`
	Baud        int    `json:"baud"`
	Rule        string `json:"rule"`
	Description string `json:"description"`
}

// ProbeSkip 记录「这个端口本轮没有被真正探测」及其原因。
//
// 为什么必须有它：此前「因故没探测」与「探测完但没有规则命中」都表现为空
// results，于是「端口被别的会话占着」「串口打不开」「超出预算没轮到」统统被
// 读成「没有设备」——设备明明在线也会报「未检测到已知设备」，排查者会去怀疑
// 接线/波特率/地址/CRC。两条产生空结果的静默路径：
//  1. 端口已被会话占用（原先 `continue`，无任何说明）
//  2. ProbePorts 内部 open/read/write 失败（原先裸 `continue`，连日志都没有）
type ProbeSkip struct {
	Port   string `json:"port"`
	Reason string `json:"reason"`
}

// ProbeOutcome 是一次探测的完整结果。
type ProbeOutcome struct {
	Results   []ProbeResult `json:"results"`
	Skipped   []ProbeSkip   `json:"skipped"`
	Attempts  int           `json:"attempts"`
	ElapsedMs int64         `json:"elapsedMs"`
	// BudgetExhausted 为真表示到点即停，后面还有端口/波特率没试——调用方
	// 必须据此判断「结果为空」不等于「没有设备」。
	BudgetExhausted bool `json:"budgetExhausted"`
	// Busy 为真表示同一时刻已有另一次探测在跑，本次完全未执行。
	Busy bool `json:"busy"`
}

// defaultProbeBudget 是一次探测的总时间预算。
//
// 为什么需要：probeRead 对「静默端口」每次尝试都要跑满预算下限（默认
// timeout_ms=200 → 1s），而默认配置是 3 规则 × 7 波特率 = 21 次尝试，
// 设备不应答时必然超过 20s；而 client.Call 的请求超时只有 10s，于是 CLI
// 先放弃、守护进程还在扫，调用方拿到的是 request timeout 而不是探测结论。
// 这里给整次调用（含所有端口）一个上限，到点返回已有结果并显式说明还有
// 哪些没试，保证「再慢也有结论」。
//
// 取值权衡：设备不应答时每个波特率约 3s（3 条规则），12s 能覆盖默认排序下
// 的前 4 档（115200/9600/19200/38400）——实践中最常见的几档；再往上必然
// 要等更久，而等更久换来的收益远小于「立刻给结论并说清没试哪些」。
// 调用方可用 budgetMs 覆盖。
// 默认预算取值：设备不应答时每次尝试花 max(3×timeout_ms, 300ms)。
// 默认 timeout_ms=200 → 每次 600ms → 每档（3 条规则）1.8s → 全部 7 档约 12.6s
// 加上开关串口的开销，15s 能覆盖默认 baud_rates 的全部档位。
const defaultProbeBudget = 15 * time.Second

// probeMu 保证同一时刻只有一次探测在跑。并发的第二个探测原先会因端口被占
// 而静默返回空结果，现在直接说明「另一个探测正在进行」，不再伪装成「没有设备」。
var probeMu sync.Mutex

// probeNow 是探测用的时钟，测试里可替换，用来确定性地验证预算逻辑
// （真实时钟在纳秒/毫秒粒度下无法稳定构造「刚好超预算」的时刻）。
var probeNow = time.Now

// ---- config loading ----

func LoadProbeConfig(path string) (*ProbeConfig, error) {
	var data []byte
	if path == "" {
		// Nothing on disk: fall back to the embedded rule set so the probe
		// feature works without a shipped config/ directory.
		data = config.DefaultProbeToml()
	} else {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}
	var cfg ProbeConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 TOML 配置失败: %w", err)
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 200
	}
	if len(cfg.BaudRates) == 0 {
		cfg.BaudRates = []int{115200}
	}
	return &cfg, nil
}

// findProbeConfig 按优先级查找探测配置文件：
// 1. 显式路径
// 2. daemon 可执行文件旁 probe.toml
// 3. <exe>/config/probe.toml（发布包：exe 与 config 子目录同级）
// 4. <exe>/../config/probe.toml（exe 在 bin 子目录：config 与 bin 同级）
// 5. <exe>/../../config/probe.toml（开发：exe 在 build/bin，config 在仓库根）
// 6. 当前工作目录 probe.toml / config/probe.toml
//
// 前 5 项都基于可执行文件位置，因此与启动时的 cwd 无关；第 6 项是兜底。
// 这样发布包无论从哪个目录启动（快捷方式、任务栏固定、其它 cwd 调用
// serial-cli）都能读到随包的 config/probe.toml。
//
// 都找不到时返回空路径且不报错，调用方回退到内置规则
// （config.DefaultProbeToml），因此 probe 功能开箱即用。
func findProbeConfig(explicitPath string) (string, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath, nil
		}
		return "", fmt.Errorf("指定配置文件不存在: %s", explicitPath)
	}

	candidates := probeConfigCandidates(exeDirOrEmpty(), wdOrEmpty())

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", nil
}

// probeConfigCandidates 生成探测配置的候选路径（按优先级），并去重。
// exeDir / wd 为空字符串时跳过对应分组，便于单元测试覆盖布局分支。
func probeConfigCandidates(exeDir, wd string) []string {
	candidates := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}

	if exeDir != "" {
		add(filepath.Join(exeDir, "probe.toml"))
		add(filepath.Join(exeDir, "config", "probe.toml"))
		add(filepath.Join(exeDir, "..", "config", "probe.toml"))
		add(filepath.Join(exeDir, "..", "..", "config", "probe.toml"))
	}
	if wd != "" {
		add(filepath.Join(wd, "probe.toml"))
		add(filepath.Join(wd, "config", "probe.toml"))
	}
	return candidates
}

// exeDirOrEmpty 返回 daemon 可执行文件所在目录，取不到时返回空串。
func exeDirOrEmpty() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// wdOrEmpty 返回当前工作目录，取不到时返回空串。
func wdOrEmpty() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// ---- probe engine ----

// ProbePorts 对给定端口列表执行设备探测。
//
// occupiedPorts 为已占用的端口集合；这些端口**不会被静默忽略**，而是记进
// Skipped 并说明原因。指定 baudRates 覆盖配置中的默认波特率；为空则使用配置
// 文件中的。指定 ruleNames 过滤规则；为空则使用全部规则。budget 为整次调用的
// 总时间预算，<=0 时取 defaultProbeBudget。
//
// 空 results 不再等价于「没有设备」：调用方必须同时看 Skipped 与
// BudgetExhausted（见 ProbeOutcome 的说明）。
func ProbePorts(ports []string, occupiedPorts map[string]bool, cfg *ProbeConfig, baudRates []int, ruleNames []string, budget time.Duration) (out ProbeOutcome) {
	out = ProbeOutcome{Results: []ProbeResult{}, Skipped: []ProbeSkip{}}

	start := probeNow()
	// 必须用命名返回值：defer 在 return 之后才执行，改一个局部变量的字段是改不到
	// 返回值上的（实测 elapsedMs 会恒为 0）。
	defer func() { out.ElapsedMs = probeNow().Sub(start).Milliseconds() }()

	skipAll := func(reason string) {
		for _, p := range ports {
			out.Skipped = append(out.Skipped, ProbeSkip{Port: p, Reason: reason})
		}
	}

	if cfg == nil {
		skipAll("没有可用的探测配置")
		return out
	}
	if budget <= 0 {
		budget = defaultProbeBudget
	}

	// 同一时刻只允许一次探测。并发的第二个探测此前会因端口被占而返回空结果，
	// 与「没有设备」无法区分；现在明确说明本次未执行。
	if !probeMu.TryLock() {
		out.Busy = true
		skipAll("已有另一次设备探测正在进行，本次未执行")
		logOp("操作", "探测跳过：已有另一次探测在进行，请求端口 %d 个未执行", len(ports))
		return out
	}
	defer probeMu.Unlock()

	deadline := start.Add(budget)

	// 编译规则过滤器
	ruleSet := make(map[string]bool, len(ruleNames))
	for _, n := range ruleNames {
		ruleSet[strings.TrimSpace(n)] = true
	}
	filterRules := len(ruleSet) > 0

	// 编译端口正则
	compiledRules := make([]struct {
		rule    ProbeRule
		portRE  *regexp.Regexp
		matchRE *regexp.Regexp
	}, 0, len(cfg.Rules))

	for _, r := range cfg.Rules {
		if filterRules && !ruleSet[r.Name] {
			continue
		}
		portRE, err := regexp.Compile(r.PortPattern)
		if err != nil {
			portRE = nil
		}
		var matchRE *regexp.Regexp
		if r.MatchType == "regex" && r.MatchValue != "" {
			matchRE, err = regexp.Compile(r.MatchValue)
			if err != nil {
				matchRE = nil
			}
		}
		compiledRules = append(compiledRules, struct {
			rule    ProbeRule
			portRE  *regexp.Regexp
			matchRE *regexp.Regexp
		}{r, portRE, matchRE})
	}

	if len(compiledRules) == 0 {
		if filterRules {
			names := make([]string, 0, len(ruleSet))
			for n := range ruleSet {
				names = append(names, n)
			}
			skipAll(fmt.Sprintf("配置里没有名为 %s 的规则", strings.Join(names, ", ")))
		} else {
			skipAll("探测配置里没有任何规则")
		}
		return out
	}

	bauds := baudRates
	if len(bauds) == 0 {
		bauds = cfg.BaudRates
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	for pi, portName := range ports {
		// 预算到点：剩下的端口一个都还没试，必须显式列出来，
		// 否则调用方会把「没轮到」当成「没有设备」。
		if probeNow().After(deadline) {
			for _, rest := range ports[pi:] {
				out.Skipped = append(out.Skipped, ProbeSkip{
					Port:   rest,
					Reason: fmt.Sprintf("超出总预算 %s，未探测（前面已用 %s）", budget, probeNow().Sub(start).Round(time.Millisecond)),
				})
			}
			out.BudgetExhausted = true
			logOp("操作", "探测提前结束：总预算 %s 用尽，%d/%d 个端口未探测", budget, len(ports)-pi, len(ports))
			break
		}

		if occupiedPorts[portName] {
			out.Skipped = append(out.Skipped, ProbeSkip{Port: portName, Reason: "端口已被会话占用，未探测"})
			logOp("操作", "探测跳过 %s：端口已被会话占用", portName)
			continue
		}

		// port_pattern 过滤
		rulesForPort := make([]struct {
			rule    ProbeRule
			matchRE *regexp.Regexp
		}, 0)
		for _, cr := range compiledRules {
			if cr.portRE == nil || cr.portRE.MatchString(portName) {
				rulesForPort = append(rulesForPort, struct {
					rule    ProbeRule
					matchRE *regexp.Regexp
				}{cr.rule, cr.matchRE})
			}
		}
		if len(rulesForPort) == 0 {
			out.Skipped = append(out.Skipped, ProbeSkip{Port: portName, Reason: "没有与该端口匹配的规则（port_pattern）"})
			continue
		}

		// 逐波特率尝试（命中后跳出）
		portMatched := false
		openFails := 0
		var lastOpenErr error
		writeFails := 0
		readFails := 0
		var lastReadErr error
		bytesSeen := 0
	baudLoop:
		for bi, baud := range bauds {
			if portMatched {
				break
			}
			// 预算到点：剩下的波特率没试，同样要说明。
			if probeNow().After(deadline) {
				out.Skipped = append(out.Skipped, ProbeSkip{
					Port: portName,
					Reason: fmt.Sprintf("超出总预算 %s：已试波特率 %v，剩余 %d 个（%v）未试",
						budget, bauds[:bi], len(bauds)-bi, bauds[bi:]),
				})
				out.BudgetExhausted = true
				logOp("操作", "探测 %s 提前结束：总预算 %s 用尽，剩余 %d 个波特率未试", portName, budget, len(bauds)-bi)
				break baudLoop
			}

			p, err := openSerialPort(&SerialConfig{
				Port: portName, Baud: baud, DataBits: 8, StopBits: "1", Parity: "none",
			})
			if err != nil {
				openFails++
				lastOpenErr = err
				continue
			}
			p.ResetInputBuffer()
			p.ResetOutputBuffer()

			// 逐规则发送探测帧
			for _, cr := range rulesForPort {
				probeBytes, err := hex.DecodeString(cr.rule.ProbeHex)
				if err != nil {
					continue
				}

				if _, werr := p.Write(probeBytes); werr != nil {
					writeFails++
					continue
				}

				p.SetReadTimeout(timeout)
				out.Attempts++
				resp, rerr := probeRead(p, timeout)
				if rerr != nil {
					readFails++
					lastReadErr = rerr
				}
				if len(resp) > 0 {
					bytesSeen += len(resp)
				}

				if len(resp) < cr.rule.MinResponseLen {
					continue
				}

				respHex := strings.ToUpper(hex.EncodeToString(resp))
				if matchProbeResponse(respHex, string(resp), &cr.rule, cr.matchRE) {
					out.Results = append(out.Results, ProbeResult{
						Port:        portName,
						Baud:        baud,
						Rule:        cr.rule.Name,
						Description: fmt.Sprintf("%s @ %d baud", cr.rule.Name, baud),
					})
					portMatched = true
					p.Close()
					break baudLoop
				}
			}
			p.Close()
		}

		// 没命中，但一句「未检测到」未必是实情：把「压根没探成」的原因说清楚。
		// 这一条正是原先连日志都没有的静默路径。
		if !portMatched {
			switch {
			case openFails == len(bauds) && lastOpenErr != nil:
				reason := fmt.Sprintf("串口在所有 %d 个波特率上都打不开（最后一个错误: %v）——端口可能正被其它程序/上一次未结束的探测占用", openFails, lastOpenErr)
				out.Skipped = append(out.Skipped, ProbeSkip{Port: portName, Reason: reason})
				logOp("操作", "探测跳过 %s：%s", portName, reason)
			case bytesSeen == 0 && readFails > 0:
				reason := fmt.Sprintf("串口打开成功但读取失败 %d 次（最后一个错误: %v）", readFails, lastReadErr)
				out.Skipped = append(out.Skipped, ProbeSkip{Port: portName, Reason: reason})
				logOp("操作", "探测跳过 %s：%s", portName, reason)
			case bytesSeen == 0 && writeFails > 0:
				reason := fmt.Sprintf("探测帧写入失败 %d 次，设备未收到任何探测数据", writeFails)
				out.Skipped = append(out.Skipped, ProbeSkip{Port: portName, Reason: reason})
				logOp("操作", "探测跳过 %s：%s", portName, reason)
			}
		}
	}

	return out
}

// probeRead 在一次探测里把响应读完整。
//
// 为什么必须循环而不是单次 Read：Windows 下 go.bug.st/serial 的 Read 一有字节
// 就返回（其实现里是 `if readed > 0 { return }`），所以一次 Read 常常只拿到
// 响应的头一两个字节，后面的还在路上。此前 ProbePorts 只 Read 一次，导致：
//   - 多字节响应被截断，min_response_len 与匹配双双落空——设备明明应答了却
//     报「未检测到已知设备」
//   - modbus_crc 类规则对多字节响应**一律失效**（校验 CRC 需要完整帧）
//
// 终止条件写成显式的，不依赖 SetReadTimeout 在库内的具体行为：该库为 CH340
// 打过补丁（其 SetReadTimeout 注释写明「最高位被置位的值会让 CH340 驱动表现得
// 像超时 0，最坏情况变成自旋」），而本工具的目标设备大量使用 CH340/CH343。
//   - 尚未收到任何数据：只要没超总预算就继续等（设备可能晚应答）
//   - 已收到数据但本轮读空：认为这一帧已收完，立即结束
//
// 总预算取 max(3×timeout, 300ms)。
//
// 这个预算的实际含义只有一个：**最多等多久才收到第一个字节**。
// 因为循环里 `len(resp) > 0` 排在 `After(deadline)` 之前——一旦收到过任何字节，
// 下一个空读就结束，跟预算多大无关；帧的收尾由 SetReadTimeout 给的读窗口决定。
//
// 所以「预算」完全由沉默端口承担成本。此前这里写的是 max(3×timeout, 1s)，
// 那个 1s 下限在默认 timeout_ms=200 下把每次尝试从 600ms 抬到 1000ms：
// 每档 3s、7 档 21s，超过当时的 12s 总预算，导致默认列表里后 3 档
// （含 230400/460800/921600）**永远轮不到**。下限降到 300ms 只是防呆
// （防止有人把 timeout_ms 填成个位数），默认配置下不再生效。
// 参数 r 用接口而不是具体类型，便于用管道冒充串口做单元测试。
//
// 返回读取过程中遇到的**第一个真实错误**（不含「读空」，静默端口就是读空）。
// 此前这个错误被直接丢弃，是「空结果 = 没有设备」的成因之一：串口打开成功但
// 根本读不了（USB 拔出、句柄被别人抢走等）时，用户只会看到「未检测到已知设备」。
func probeRead(r io.Reader, timeout time.Duration) ([]byte, error) {
	var resp []byte
	var firstErr error
	buf := make([]byte, 256)

	budget := 3 * timeout
	if budget < 300*time.Millisecond {
		budget = 300 * time.Millisecond
	}
	deadline := time.Now().Add(budget)

	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			resp = append(resp, buf[:n]...)
			if len(resp) >= len(buf) {
				break
			}
			continue
		}
		if rerr != nil {
			if firstErr == nil {
				firstErr = rerr
			}
			break
		}
		// n == 0 且无错：本轮读空。已收到数据即认为帧结束。
		if len(resp) > 0 || time.Now().After(deadline) {
			break
		}
	}
	return resp, firstErr
}

// matchProbeResponse 根据规则匹配响应。
func matchProbeResponse(hexResp, textResp string, rule *ProbeRule, matchRE *regexp.Regexp) bool {
	switch rule.MatchType {
	case "substring":
		// match_value 在响应 hex 中作为子串出现
		mv := strings.ToUpper(strings.ReplaceAll(rule.MatchValue, " ", ""))
		return strings.Contains(hexResp, mv)

	case "regex":
		if matchRE == nil {
			return false
		}
		return matchRE.MatchString(textResp)

	case "modbus_crc":
		data, err := hex.DecodeString(hexResp)
		if err != nil || len(data) < 4 {
			return false
		}
		return checkCRC16(data)

	default:
		return false
	}
}

// ---- Modbus CRC16 ----

func crc16Modbus(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func checkCRC16(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	payload := data[:len(data)-2]
	expected := uint16(data[len(data)-1])<<8 | uint16(data[len(data)-2]) // LE
	computed := crc16Modbus(payload)
	return computed == expected
}
