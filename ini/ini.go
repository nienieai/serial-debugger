// Package ini 只做一件事：把一个 INI 文件读成 map[section]map[key]value。
//
// 为什么不复用 GUI 的设置解析：main 包那份带反射、负责「结构体 ↔ INI」的双向
// 映射，CLI 与守护进程只需要「读几个键」，共享它会把两个小程序的依赖拉到 GUI
// 的整个结构体上。这里保持零依赖、只读。
package ini

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DirName 是设置目录名，与各 exe 同级。
const DirName = "settings"

// Dir 返回 exeDir 下的设置目录。
func Dir(exeDir string) string { return filepath.Join(exeDir, DirName) }

// Path 返回设置目录下某个文件的完整路径。
func Path(exeDir, name string) string { return filepath.Join(Dir(exeDir), name) }

// File 是解析后的 INI 内容。section 与 key 一律转小写，取值保持原样
// （去掉首尾空白；成对引号会被剥掉）。
type File map[string]map[string]string

// Load 打开并解析 path。文件不存在时返回的错误可用 os.IsNotExist 判断。
func Load(path string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse 从 r 解析 INI。
//
// 规则刻意保守：空行与 # / ; 开头的行忽略，无法识别的行跳过（不报错），
// 值里的 ; 或 # 不当行内注释处理——路径之类的内容不该被悄悄截断。
func Parse(r io.Reader) (File, error) {
	m := File{}
	section := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		// Windows 记事本、PowerShell 的 Set-Content 会在文件头写 UTF-8 BOM，
		// 不去掉的话首行 `[section]` 会因多一个 \ufeff 而被当作无效行。
		line := strings.TrimPrefix(sc.Text(), "\ufeff")
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				continue
			}
			section = strings.ToLower(strings.TrimSpace(line[1:end]))
			if m[section] == nil {
				m[section] = map[string]string{}
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := unquote(strings.TrimSpace(line[eq+1:]))
		if m[section] == nil {
			m[section] = map[string]string{}
		}
		m[section][key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Get 取 section.key，缺失时返回 def。
func (f File) Get(section, key, def string) string {
	if f == nil {
		return def
	}
	if sec, ok := f[strings.ToLower(section)]; ok {
		if v, ok := sec[strings.ToLower(key)]; ok {
			return v
		}
	}
	return def
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
