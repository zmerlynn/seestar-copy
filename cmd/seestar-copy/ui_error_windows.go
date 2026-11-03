//go:build gui

package main

func showStartupError(err error) { messageBox(startupErrorTitle, err.Error()) }
