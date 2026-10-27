package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/*.html frontend/*.css frontend/*.js frontend/*.svg
var assets embed.FS

func main() {
	web, err := fs.Sub(assets, "frontend")
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp()
	title, width, height := "protocol-simulator模拟主站", 1090, 845
	minWidth, minHeight := 1080, 720
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--gylab-native-window=") {
			app.remote = &nativeRemote{address: os.Getenv("GYLAB_NATIVE_ADDRESS"), token: os.Getenv("GYLAB_NATIVE_TOKEN"), session: strings.TrimPrefix(arg, "--gylab-native-window=")}
		}
	}
	if app.remote == nil {
		config, e := projectConfigFromArgs(os.Args[1:])
		if e != nil {
			log.Fatal(e)
		}
		if config != nil {
			if e = app.initializeProjectWindow(*config); e != nil {
				log.Fatal(e)
			}
		}
		if app.workspaceRole == "server" {
			title = "protocol-simulator模拟子站"
		}
	}
	if app.remote != nil {
		c, e := app.GetNativeContext()
		if e != nil {
			return
		}
		app.remote.kind = c.Kind
		title = windowTitle(c)
		width, height = 430, 760
		minWidth, minHeight = 380, 300
		switch c.Kind {
		case "telemetry-values-dialog":
			width, height = 540, 390
			minWidth, minHeight = 500, 350
		case "relation-dialog":
			width, height = 1100, 540
			minWidth, minHeight = 900, 420
		case "remote-config-dialog":
			width, height = 760, 530
			minWidth, minHeight = 680, 400
			if strings.TrimPrefix(c.PointKind, "tab:") == "yt" {
				height = 430
			}
		case "auto-data-dialog":
			width, height = 1080, 560
			minWidth, minHeight = 860, 400
		case "channel-dialog":
			width, height = 380, 650
			minWidth, minHeight = 280, 110
		case "device-dialog":
			width, height = 660, 700
			minWidth, minHeight = 640, 300
		case "auto-yk-dialog":
			width, height = 760, 380
			minWidth, minHeight = 740, 340
		case "auto-yt-dialog":
			width, height = 536, 326
			minWidth, minHeight = 516, 286
		case "auto-bulk-dialog":
			width, height = 210, 270
			minWidth, minHeight = 180, 240
		case "confirm-dialog":
			width, height = 460, 285
			minHeight = 220
		case "point-dialog":
			height = 480
		case "control-dialog":
			height = 580
			if c.Point == "setpoint" || c.Point == "step" {
				height = 430
				minHeight = 420
			}
		case "events-dialog":
			width, height = 950, 550
			minWidth, minHeight = 700, 300
		case "manager-dialog":
			width, height = 800, 500
			minWidth = 650
		}
	} else {
		app.native, err = newNativeWindows(app)
		if err != nil {
			log.Fatal(err)
		}
	}
	isBulk := app.remote != nil && app.remote.kind == "auto-bulk-dialog"
	err = wails.Run(&options.App{AlwaysOnTop: isBulk, DisableResize: isBulk, Title: title, Width: width, Height: height, MinWidth: minWidth, MinHeight: minHeight, BackgroundColour: &options.RGBA{R: 23, G: 26, B: 32, A: 255}, Windows: &windows.Options{Theme: windows.Dark}, Mac: &mac.Options{TitleBar: mac.TitleBarDefault(), Appearance: mac.NSAppearanceNameDarkAqua, DisableEscapeExitsFullscreen: true}, AssetServer: &assetserver.Options{Assets: web}, OnStartup: app.startup, OnDomReady: app.domReady, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}})
	if err != nil {
		log.Fatal(err)
	}
}
