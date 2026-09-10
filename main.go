package main

import (
	"embed"
	"encoding/json"
	"log"

	"github.com/vekhyat/Auralis/backend"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed wails.json
var wailsJSON []byte

func main() {

	type wailsInfo struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	var config wailsInfo
	if err := json.Unmarshal(wailsJSON, &config); err == nil && config.Info.ProductVersion != "" {
		backend.AppVersion = config.Info.ProductVersion
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Auralis",
		Width:     1280,
		Height:    800,
		MinWidth:  1200,
		MinHeight: 720,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "auralis-desktop",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				backend.HandleProtocolArgs(data.Args)
			},
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
		},
	})

	if err != nil {
		log.Fatal("Error:", err.Error())
	}
}
