package sessionfile

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// ExportHexPerLine 是导出文本里一行十六进制承载的字节数。
//
// 32 B = 64 个十六进制字符。会话文件里的帧可以到 4016 B（8032 字符），塞成一行
// 的话记事本、日志查看器都很难读，所以按这个宽度折行、续行缩进对齐。
const ExportHexPerLine = 32

// exportLineEnding 用 CRLF：导出的第一用途是丢给 Windows 记事本看。
const exportLineEnding = "\r\n"

// WriteText 把整份会话导成「一行一帧」的纯文本，返回写出的字节数。
//
// 只做格式化，不做 IO 决策：写到文件、stdout 还是网络流由调用方决定（GUI 走
// 保存对话框，CLI 直接写 stdout 或 -o）。信息不足或损坏的部分不猜——回放帧时
// 已经由 ReadFile 判定过，这里只照着 Frames 打。
//
// 长度字段是 payload 字节数；系统帧（dir=sys）的 payload 本来就是文案，按文本打，
// 不再十六进制化。
func WriteText(w io.Writer, info *FileInfo) (int64, error) {
	if info == nil {
		return 0, fmt.Errorf("stlog: nil FileInfo")
	}
	bw := bufio.NewWriterSize(w, 64*1024)
	var n int
	write := func(format string, args ...any) error {
		c, err := fmt.Fprintf(bw, format, args...)
		n += c
		return err
	}

	// 头：以 # 开头，方便 grep 时一眼区分元信息与数据行。
	if err := write("# .stlog 导出（v1.4 格式）%s", exportLineEnding); err != nil {
		return int64(n), err
	}
	if err := write("# 文件: %s%s", info.Path, exportLineEnding); err != nil {
		return int64(n), err
	}
	if s := info.Settings; s != nil {
		line := fmt.Sprintf("# 设置: uuid=%s created=%s", s.UUID, s.Created)
		if s.Label != "" {
			line += " label=" + s.Label
		}
		if s.Port != "" {
			line += " port=" + s.Port
		}
		if s.Baud > 0 {
			line += fmt.Sprintf(" baud=%d", s.Baud)
		}
		if err := write("%s%s", line, exportLineEnding); err != nil {
			return int64(n), err
		}
	}
	if err := write("# 大小: %d B   页: %d   记录: %d   帧: %d   rx/tx: %d / %d B%s",
		info.Size, len(info.Pages), info.Records, len(info.Frames),
		info.RxTotal, info.TxTotal, exportLineEnding); err != nil {
		return int64(n), err
	}
	// 转发录制：统计块只有 rx/tx 两栏，方向字节数按记录现加。
	if info.Settings.IsForward() {
		if err := write("# 模式: 端口转发 %s ↔ %s   A→B/B→A: %d / %d B%s",
			info.Settings.Port, info.Settings.PortB,
			info.P1Total, info.P2Total, exportLineEnding); err != nil {
			return int64(n), err
		}
	}
	if note := exportNote(info); note != "" {
		if err := write("# 状态: %s%s", note, exportLineEnding); err != nil {
			return int64(n), err
		}
	}
	// 来源列宽按整份文件里最长的来源算；全都没来源就不占位（大部分 rx 是空的，
	// 每行留 14 个空格纯属浪费）。来源是 ASCII token（gui-… / cli-… / auto /
	// multistr），所以按字节补位与显示宽度一致。
	originW := 0
	for i := range info.Frames {
		if n := len(info.Frames[i].OriginStr); n > originW {
			originW = n
		}
	}
	if originW > 0 && originW < 8 {
		originW = 8
	}

	colNote := "# 列: 时间 dir 长度 十六进制；时间戳为本地时间（毫秒）"
	if info.Settings.IsForward() {
		colNote += "；转发录制里 p1 = A→B，p2 = B→A"
	}
	if originW > 0 {
		colNote += "；dir 与长度之间是来源（仅 Tx 有值：gui-… / cli-… / auto / multistr）"
	}
	if err := write("%s；十六进制每行 %d B，续行缩进对齐%s",
		colNote, ExportHexPerLine, exportLineEnding); err != nil {
		return int64(n), err
	}

	for i := range info.Frames {
		f := &info.Frames[i]
		ts := time.UnixMilli(int64(f.Ts)).Format("2006-01-02 15:04:05.000")
		dir := exportDirName(f.Dir)
		prefix, indent := exportPrefix(ts, dir, f.OriginStr, originW, len(f.Payload))
		if DirBase(f.Dir) == DirSystem {
			text := strings.ReplaceAll(string(f.Payload), "\r", "\\r")
			text = strings.ReplaceAll(text, "\n", "\\n")
			if err := write("%s%s%s", prefix, text, exportLineEnding); err != nil {
				return int64(n), err
			}
			continue
		}
		if err := writeFrameHex(write, prefix, indent, f.Payload); err != nil {
			return int64(n), err
		}
	}
	if err := bw.Flush(); err != nil {
		return int64(n), err
	}
	return int64(n), nil
}

// exportPrefix 拼一帧的行首（时间 / 方向 / [来源] / 长度），同时给出续行缩进。
// originW == 0 表示整份文件都没有来源，这一列不出现。
func exportPrefix(ts, dir, origin string, originW, payloadLen int) (string, string) {
	p := fmt.Sprintf("%s  %-4s  ", ts, dir)
	if originW > 0 {
		p += fmt.Sprintf("%-*s  ", originW, origin)
	}
	p += fmt.Sprintf("%6d  ", payloadLen)
	return p, strings.Repeat(" ", len(p))
}

// writeFrameHex 打一帧的十六进制，按 ExportHexPerLine 折行；续行用行首等宽的缩进。
func writeFrameHex(write func(string, ...any) error, prefix, indent string, payload []byte) error {
	hexStr := strings.ToUpper(hex.EncodeToString(payload))
	width := ExportHexPerLine * 2
	if len(hexStr) == 0 {
		return write("%s%s", prefix, exportLineEnding)
	}
	for off := 0; off < len(hexStr); off += width {
		end := off + width
		if end > len(hexStr) {
			end = len(hexStr)
		}
		lead := prefix
		if off > 0 {
			lead = indent
		}
		if err := write("%s%s%s", lead, hexStr[off:end], exportLineEnding); err != nil {
			return err
		}
	}
	return nil
}

// exportDirName 与 CLI 的 stlogDirName、前端面板用的是同一套名字。
func exportDirName(d uint8) string {
	switch DirBase(d) {
	case DirRx:
		return "rx"
	case DirTx:
		return "tx"
	case DirP1:
		return "p1"
	case DirP2:
		return "p2"
	default:
		return "sys"
	}
}

// exportNote 把读出来的异常汇总成一行，没有异常就返回空串。
func exportNote(info *FileInfo) string {
	var parts []string
	if info.Truncated {
		parts = append(parts, "末帧未结束")
	}
	if info.BrokenChain {
		parts = append(parts, "续片链在页边界被切断")
	}
	if len(info.Recovered) > 0 {
		offs := make([]string, 0, len(info.Recovered))
		for _, off := range info.Recovered {
			offs = append(offs, fmt.Sprintf("0x%X", off))
		}
		parts = append(parts, "已从影子页恢复: "+strings.Join(offs, ", "))
	}
	if info.StoppedAt != 0 {
		msg := fmt.Sprintf("停在页偏移 0x%X", info.StoppedAt)
		if info.StopErr != nil {
			msg += "（" + info.StopErr.Error() + "）"
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, "；")
}
