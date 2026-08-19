//go:build windows

package main

import (
	"fmt"
	"reflect"
	"unsafe"

	"go.bug.st/serial"
	"golang.org/x/sys/windows"
)

const (
	_fOutxCtsFlow    = 0x0004
	_fInX            = 0x0080
	_fOutX           = 0x0100
	_fRtsControlMask = 0x3000
	_rtsEnable       = 0x1000
	_rtsHandshake    = 0x2000
)

func setFlowControl(p serial.Port, mode string) error {
	v := reflect.ValueOf(p)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	f := v.FieldByName("handle")
	if !f.IsValid() {
		return fmt.Errorf("setFlowControl: cannot access port handle")
	}
	h := windows.Handle(f.Uint())

	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		return fmt.Errorf("setFlowControl GetCommState: %w", err)
	}

	switch mode {
	case "none":
		dcb.Flags &^= _fOutxCtsFlow | _fInX | _fOutX
		dcb.Flags &^= _fRtsControlMask
		dcb.Flags |= _rtsEnable
	case "rts_cts":
		dcb.Flags |= _fOutxCtsFlow
		dcb.Flags &^= _fInX | _fOutX
		dcb.Flags &^= _fRtsControlMask
		dcb.Flags |= _rtsHandshake
	case "xon_xoff":
		dcb.Flags |= _fInX | _fOutX
		dcb.Flags &^= _fOutxCtsFlow
		dcb.Flags &^= _fRtsControlMask
		dcb.Flags |= _rtsEnable
	default:
		return nil
	}

	return windows.SetCommState(h, &dcb)
}
