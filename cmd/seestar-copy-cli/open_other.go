//go:build !windows

package main

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// openURL runs the system's opener on url, without stdin, stdout or stderr,
// so a text-mode browser cannot take over the terminal.
func openURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "/usr/bin/open"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, url).Run()
}
