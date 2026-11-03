//go:build gui

package main

// windowErrorHandler shows a fatal webview error with MessageBox, since no
// window exists to show it in.
var windowErrorHandler = fatalWindowError(func(text string) { messageBox("seestar-copy", text) })
