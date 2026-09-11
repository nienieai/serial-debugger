//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func loadPortDescriptions() map[string]string {
	result := make(map[string]string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write PowerShell output to a temp UTF-8 file to avoid code-page garbling
	tmpFile := os.TempDir() + "\\serial_ports_" + fmt.Sprint(time.Now().UnixNano()) + ".txt"
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"Get-CimInstance -ClassName Win32_PnPEntity | "+
			"Where-Object { $_.Name -match '\\(COM[0-9]+\\)' } | "+
			"ForEach-Object { $_.Name } | "+
			"Out-File -FilePath '"+tmpFile+"' -Encoding UTF8")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var output []byte
	if runErr := cmd.Run(); runErr == nil {
		data, ferr := os.ReadFile(tmpFile)
		os.Remove(tmpFile)
		if ferr == nil {
			output = data
		}
	}

	if len(output) == 0 {
		// Fallback: try wmic
		ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel2()
		cmd2 := exec.CommandContext(ctx2, "wmic", "path", "Win32_PnPEntity",
			"where", "Name like '%(COM%'", "get", "Name")
		cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, _ = cmd2.Output()
	}

	// PowerShell 5.1's `Out-File -Encoding UTF8` writes a UTF-8 BOM and
	// os.ReadFile hands it back verbatim. Left in place it lands inside a
	// port description; because the results are collected into a map, which
	// port carries the stray U+FEFF varies between runs, and it breaks
	// regex/equality matching against descriptions.
	output = bytes.TrimPrefix(output, []byte{0xEF, 0xBB, 0xBF})

	re := regexp.MustCompile(`^\s*(.*?)\s*\((COM\d+)\)\s*$`)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "Name" {
			continue
		}
		if m := re.FindStringSubmatch(line); len(m) == 3 {
			result[m[2]] = strings.TrimSpace(m[1])
		}
	}
	return result
}
