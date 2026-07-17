//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbIconError = 0x00000010
	mbOK        = 0x00000000
)

// showFatalError displays a native Windows message box. This only matters
// for a windowsgui build (no console window): without it, a startup
// failure before the HTTP server exists would otherwise be completely
// invisible to the user.
func showFatalError(title, message string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	msgPtr, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(msgPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbIconError|mbOK),
	)
}
