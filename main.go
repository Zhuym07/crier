// Command crier 是"传令"的桌面外壳：首次配置与日常查看都在这里，
// 打印、收信、整理逻辑由 internal/crier 负责。
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"

	"github.com/Zhuym07/crier/internal/crier"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var frontendFiles embed.FS

func main() {
	// 开机自启项（HKCU\...\Run）带 --autostart 启动，此时收进任务栏而不是抢焦点。
	autostart := flag.Bool("autostart", false, "开机自启时最小化启动")
	flag.Parse()

	// 界面资源直接内嵌，用户机器上不需要额外的静态文件。
	assets, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		log.Fatalf("界面资源缺失: %v", err)
	}

	svc, err := crier.Open()
	if err != nil {
		log.Fatalf("打开配置失败: %v", err)
	}

	app := newApp(svc, *autostart)
	err = wails.Run(&options.App{
		Title:            "传令 Crier",
		Width:            1080,
		Height:           760,
		MinWidth:         920,
		MinHeight:        620,
		BackgroundColour: &options.RGBA{R: 244, G: 242, B: 238, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "com.github.Zhuym07.crier",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.showWindow() },
		},
	})
	if err != nil {
		log.Printf("窗口退出: %v", err)
	}
}
