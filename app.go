package main

import (
	"context"

	"github.com/Zhuym07/crier/internal/crier"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是暴露给界面的唯一后端入口。每个方法都直接转发到 internal/crier，
// 不做额外判断，保证界面看到的行为与后端一致。
type App struct {
	svc       *crier.Service
	ctx       context.Context
	autostart bool
}

func newApp(svc *crier.Service, autostart bool) *App {
	return &App{svc: svc, autostart: autostart}
}

// startup 在窗口就绪前拉起后台循环。Start 是阻塞的，必须放进 goroutine，
// 否则窗口事件循环会被堵住。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.svc.Start(ctx)
}

// domReady 把开机自启的窗口收进任务栏，用户需要时再点开。
func (a *App) domReady(ctx context.Context) {
	if a.autostart {
		runtime.WindowMinimise(ctx)
	}
}

func (a *App) showWindow() {
	if a.ctx != nil {
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) GetLastError() string { return a.svc.LastError() }

func (a *App) GetConfig() crier.Config { return a.svc.Config() }

func (a *App) SaveConfig(cfg crier.Config) error { return a.svc.Save(cfg) }

func (a *App) ListPrinters() ([]string, error) { return a.svc.Printers() }

func (a *App) TestPrint(printer string) error { return a.svc.TestPrint(printer) }

func (a *App) TestEmail(cfg crier.EmailConfig) error { return a.svc.TestEmail(cfg) }

func (a *App) TestAI(cfg crier.AIConfig) error { return a.svc.TestAI(cfg) }

func (a *App) PreviewDataURL() (string, error) { return a.svc.TestPreview() }

func (a *App) CheckNow() error { return a.svc.CheckNow() }

func (a *App) History() []crier.Entry { return a.svc.History() }

func (a *App) Acknowledge(id string) error { return a.svc.Acknowledge(id) }
