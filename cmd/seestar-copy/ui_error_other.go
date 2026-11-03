//go:build !gui || (!windows && !darwin && !linux) || (linux && !cgo)

package main

// showStartupError has nothing to show without a window: the error is
// on stderr.
func showStartupError(error) {}
