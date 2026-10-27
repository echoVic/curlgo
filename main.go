package main

import (
	"flag"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/echoVic/qingzhou-api/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	preview := flag.String("render", "", "渲染原生界面至 PNG")
	dark := flag.Bool("dark", false, "使用深色主题")
	flag.Parse()
	if *preview != "" {
		a := newApplication(core.NewWorkspace())
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
		err = png.Encode(f, ui.Render(a.view, 1200, 820, 1))
		_ = f.Close()
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	mygo.App.SetName("轻舟 API")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.WhenReady(func() {
		dir, err := os.UserConfigDir()
		if err != nil {
			mygo.Dialog.Error("无法打开本地工作区", err.Error())
			mygo.App.Quit()
			return
		}
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
		if loadErr != nil {
			a.notice = loadErr.Error()
			if readOnly {
				a.saveStatus = "数据不可读：当前为临时会话，不覆盖原文件"
			}
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "轻舟 API", Width: 1200, Height: 820, MinWidth: 940, MinHeight: 680, Content: ui.View(a.view)})
		a.updates = a.win.Update
		a.closeNative = a.win.Close
		mygo.App.OnSecondInstance(func(_ []string, _ string) {
			if !a.closed {
				a.win.Show()
				a.win.Focus()
			}
		})
		a.startSaving()
		a.win.OnClose(a.closeWindow)
		a.win.OnClosed(func() { a.closed = true; a.stop(); close(a.saves); a.engine.Close(); mygo.App.Quit() })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
