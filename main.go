package main

import (
	"context"
	"flag"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/echoVic/curlgo/internal/cli"
	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	ctx := context.Background()
	var stop context.CancelFunc
	if len(os.Args) > 1 && os.Args[1] == "run" {
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
	}
	stdin := cli.NewInterruptibleInput(ctx, os.Stdin)
	exit, handled := cli.Dispatch(ctx, stdin, os.Stdout, os.Stderr, os.Args[1:])
	if stop != nil {
		stop()
	}
	if handled {
		os.Exit(int(exit))
	}
	preview := flag.String("render", "", "渲染原生界面至 PNG")
	previewPage := flag.Int("render-page", 0, "预览页面：0 请求、1 环境、7 应用设置")
	previewWidth := flag.Int("render-width", 1200, "预览宽度（至少 940）")
	previewHeight := flag.Int("render-height", 820, "预览高度（至少 680）")
	dark := flag.Bool("dark", false, "使用深色主题")
	flag.Parse()
	if *preview != "" {
		if (*previewPage != 0 && *previewPage != 1 && *previewPage != 7) || *previewWidth < 940 || *previewHeight < 680 {
			log.Fatal("预览支持页面 0、1、7，尺寸至少为 940 × 680")
		}
		a := newApplication(core.NewWorkspace())
		defer a.engine.Close()
		a.page = *previewPage
		a.workspace.Draft.URL = "{{base_url}}/api/health"
		a.workspace.Draft.Name = "服务健康检查"
		a.workspace.Requests = []core.Request{a.workspace.Draft}
		if *dark {
			a.workspace.Settings.Theme = "深色"
		}
		f, err := os.Create(*preview)
		if err != nil {
			log.Fatal(err)
		}
		err = png.Encode(f, ui.Render(a.view, *previewWidth, *previewHeight, 1))
		_ = f.Close()
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	mygo.App.SetName("CurlGo")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	configureNativeUpdater()
	mygo.App.WhenReady(func() {
		dir, err := os.UserConfigDir()
		if err != nil {
			mygo.Dialog.Error("无法打开本地工作区", err.Error())
			mygo.App.Quit()
			return
		}
		// Keep the original data directory so existing drafts and history survive the rename.
		store, err := core.OpenStore(filepath.Join(dir, "QingzhouAPI"))
		if err != nil {
			mygo.Dialog.Error("无法打开本地工作区", err.Error())
			mygo.App.Quit()
			return
		}
		w, loadErr := store.Load()
		readOnly := w.Version == 0
		if readOnly {
			w = core.NewWorkspace()
		}
		a := newApplication(w)
		a.store = store
		a.readOnly = readOnly
		a.attachSecrets()
		if loadErr != nil {
			a.notice = loadErr.Error()
			if readOnly {
				a.saveStatus = "数据不可读：当前为临时会话，不覆盖原文件"
			}
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "CurlGo", Width: 1200, Height: 820, MinWidth: 940, MinHeight: 680, Content: ui.View(a.view)})
		a.updates = a.win.Update
		a.closeNative = a.win.Close
		a.attachUpdater(nativeUpdateBackend{})
		a.installMenu()
		mygo.App.OnSecondInstance(func(_ []string, _ string) {
			if !a.closed {
				a.win.Show()
				a.win.Focus()
			}
		})
		a.startSaving()
		offBeforeQuit := mygo.App.OnBeforeQuit(a.beforeQuit)
		a.win.OnClose(a.closeWindow)
		a.win.OnClosed(func() {
			a.closed = true
			offBeforeQuit()
			a.detachUpdater()
			a.lockSecrets()
			a.stop()
			close(a.saves)
			a.engine.Close()
			mygo.App.Quit()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// installMenu builds and installs the application menu. It is called again
// after the user changes a shortcut, so the 设置… accelerator always follows
// the resolved settings binding.
func (a *application) installMenu() {
	if a.closed || a.closing || a.win == nil {
		return
	}
	accelerator := ""
	// While a chord is being recorded the native menu must not take it: drop
	// every menu accelerator (Edit/Window roles included) and offer a quit
	// item without one, so Cmd/Ctrl+Q or Cmd/Ctrl+Z reach the recorder.
	if a.recordingShortcut != "" {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "CurlGo", Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				mygo.Separator(),
				{Label: "退出 CurlGo", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
			}},
		}))
		return
	}
	if binding, ok := a.shortcutBindingFor("settings"); ok {
		accelerator = acceleratorFor(binding.Chord)
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "CurlGo", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updaterMenuItem(),
			mygo.Separator(),
			{Label: "设置…", Accelerator: accelerator, Click: func(*mygo.MenuItem, *mygo.Window) {
				if a.closed || a.closing {
					return
				}
				a.win.Update(func() {
					if !a.closed && !a.closing {
						a.page = 7
						a.notice = ""
					}
				})
			}},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}
