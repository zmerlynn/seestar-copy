//go:build gui

package main

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// windowErrorMessage is shown when the webview can't start.
const windowErrorMessage = "Couldn't open the window. Install or repair Microsoft Edge WebView2 Runtime, then reopen seestar-copy."

// fatalWindowError gives Wails' error handler: Wails passes it runtime
// and WebView2 errors that are not fatal too, and only a fatal one, after
// which it exits, is shown.
func fatalWindowError(show func(string)) func(error) {
	return func(err error) {
		var fatal *application.FatalError
		if errors.As(err, &fatal) {
			show(windowErrorMessage)
		}
	}
}
