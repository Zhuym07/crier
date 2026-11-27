package main

import (
	"context"
	"errors"

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

// ListPlugins 返回插件页要展示的插件：内置的邮箱与 Telegram 通道，
// 加上用户在 plugins.json 里登记的外部程序。
func (a *App) ListPlugins() ([]crier.PluginInfo, error) { return a.svc.ListPlugins() }

// SavePlugin 登记或更新一个外部插件。程序路径必须来自文件选择器，
// 后端只校验它确实是本机存在的 .exe，不做任何远程下载。
func (a *App) SavePlugin(spec crier.PluginSpec) (crier.PluginInfo, error) {
	return a.svc.SavePlugin(spec)
}

func (a *App) SetPluginEnabled(id string, enabled bool) error {
	return a.svc.SetPluginEnabled(id, enabled)
}

func (a *App) RemovePlugin(id string) error { return a.svc.RemovePlugin(id) }

// PickPluginExecutable 让用户亲手选择一个本地插件程序。
// 取消时返回空字符串，不算错误。
func (a *App) PickPluginExecutable() (string, error) {
	if a.ctx == nil {
		return "", errors.New("窗口尚未就绪")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择插件程序",
		Filters: []runtime.FileFilter{
			{DisplayName: "Crier 插件 (*.exe)", Pattern: "*.exe"},
		},
	})
}

// PluginReleaseURL 是官方下载页地址，界面只展示它，打开动作走系统浏览器。
func (a *App) PluginReleaseURL() string { return crier.OfficialPluginReleaseURL }

func (a *App) OpenPluginReleasePage() error {
	if a.ctx == nil {
		return errors.New("窗口尚未就绪")
	}
	runtime.BrowserOpenURL(a.ctx, crier.OfficialPluginReleaseURL)
	return nil
}

// CheckUpdate 查询 GitHub 上的最新发布；检查失败只会出现在 Error 里，不影响使用。
func (a *App) CheckUpdate() crier.UpdateInfo {
	ctx := context.Background()
	if a.ctx != nil {
		ctx = a.ctx
	}
	return a.svc.CheckUpdate(ctx)
}

// CurrentVersion 是本次构建注入的版本号。
func (a *App) CurrentVersion() string { return a.svc.CurrentVersion() }

// DownloadUpdate 下载并校验新版安装包，返回本地路径。
func (a *App) DownloadUpdate() (string, error) {
	ctx := context.Background()
	if a.ctx != nil {
		ctx = a.ctx
	}
	return a.svc.DownloadUpdate(ctx)
}

// InstallUpdate 启动已下载的安装包，随后退出让安装程序替换文件。
func (a *App) InstallUpdate(path string) error {
	if err := a.svc.LaunchInstaller(path); err != nil {
		return err
	}
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
	return nil
}

// OpenReleasesPage 打开发布页，供人手动下载。
func (a *App) OpenReleasesPage() error {
	if a.ctx == nil {
		return errors.New("窗口尚未就绪")
	}
	runtime.BrowserOpenURL(a.ctx, crier.ReleasesPage)
	return nil
}
