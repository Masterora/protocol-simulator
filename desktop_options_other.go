//go:build !linux

package main

import "github.com/wailsapp/wails/v2/pkg/options/linux"

// Preserve the framework's existing platform defaults outside Linux.
func platformLinuxOptions() *linux.Options { return nil }

func platformWindowSize(_ string, width, height int) (int, int) { return width, height }
