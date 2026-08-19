//go:build !windows

package main

import "go.bug.st/serial"

func setFlowControl(p serial.Port, mode string) error {
	return nil
}
