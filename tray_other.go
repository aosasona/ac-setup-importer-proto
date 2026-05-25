//go:build !windows

package main

import "os"

func startTray() { select {} }
func quitApp()   { os.Exit(0) }
