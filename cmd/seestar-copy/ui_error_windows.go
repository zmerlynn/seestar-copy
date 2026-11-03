//go:build gui

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func showStartupError(err error) { messageBox(startupErrorTitle, err.Error()) }

// messageBox shows an error with MessageBox, which needs no window.
func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}
