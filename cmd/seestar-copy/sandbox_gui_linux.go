//go:build gui

package main

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

const sandboxRequirement = "Couldn't open the window: WebKit's sandbox is unavailable. Install bubblewrap and allow unprivileged user namespaces, then reopen seestar-copy."

func checkWindowSandbox() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return probeWindowSandbox(ctx, func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run()
	})
}

// probeWindowSandbox checks the sandbox before WebKit starts, since in
// WebKit a denied user namespace ends the process with SIGTRAP instead of
// an error.
func probeWindowSandbox(ctx context.Context, run func(context.Context, string, ...string) error) error {
	if err := run(ctx, "bwrap", "--ro-bind", "/", "/", "--unshare-user", "--unshare-pid", "--proc", "/proc", "--dev", "/dev", "--", "/bin/true"); err != nil {
		return errors.New(sandboxRequirement)
	}
	return nil
}
