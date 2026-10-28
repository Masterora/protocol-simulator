package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed build/appicon.png
var linuxWindowIcon []byte

func platformLinuxOptions() *linux.Options {
	return &linux.Options{Icon: linuxWindowIcon, ProgramName: "protocol-simulator", WebviewGpuPolicy: linux.WebviewGpuPolicyNever}
}

// GTK uses client sizes here; avoid applying other platforms' frame allowance.
func platformWindowSize(kind string, width, height int) (int, int) {
	switch kind {
	case "auto-yk-dialog":
		return 740, 340
	case "auto-yt-dialog":
		return 516, 286
	}
	return width, height
}
