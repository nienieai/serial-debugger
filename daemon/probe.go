package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
// occupiedPorts 为已占用的端口集合，这些端口会被跳过。
// 指定 baudRates 覆盖配置中的默认波特率；为空则使用配置文件中的。
// 指定 ruleNames 过滤规则；为空则使用全部规则。
func ProbePorts(ports []string, occupiedPorts map[string]bool, cfg *ProbeConfig, baudRates []int, ruleNames []string) []ProbeResult {
	if cfg == nil {
		return nil
	}

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
		return nil
	}

	bauds := baudRates
	if len(bauds) == 0 {
		bauds = cfg.BaudRates
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	var results []ProbeResult

	for _, portName := range ports {
		if occupiedPorts[portName] {
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
			continue
		}

		// 逐波特率尝试（命中后跳出）
		portMatched := false
	baudLoop:
		for _, baud := range bauds {
			if portMatched {
				break
			}

			p, err := openSerialPort(&SerialConfig{
				Port: portName, Baud: baud, DataBits: 8, StopBits: "1", Parity: "none",
			})
			if err != nil {
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
					continue
				}

				p.SetReadTimeout(timeout)
				buf := make([]byte, 256)
				n, _ := p.Read(buf)

				if n < cr.rule.MinResponseLen {
					continue
				}

				respHex := strings.ToUpper(hex.EncodeToString(buf[:n]))
				if matchProbeResponse(respHex, string(buf[:n]), &cr.rule, cr.matchRE) {
					results = append(results, ProbeResult{
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
	}

	return results
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
