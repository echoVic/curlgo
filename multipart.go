package main

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *application) addFormPart(kind core.FormKind) {
	if len(a.workspace.Draft.FormParts) >= core.MaxFormParts {
		a.notice = fmt.Sprintf("表单最多支持 %d 个字段", core.MaxFormParts)
		return
	}
	a.workspace.Draft.FormParts = append(a.workspace.Draft.FormParts, core.FormPart{ID: core.NewID(), Kind: kind})
	a.persist()
}

func (a *application) removeFormPart(id string) {
	for i, part := range a.workspace.Draft.FormParts {
		if part.ID == id {
			a.workspace.Draft.FormParts = append(a.workspace.Draft.FormParts[:i], a.workspace.Draft.FormParts[i+1:]...)
			a.persist()
			return
		}
	}
}

func (a *application) chooseFormFile(id string) {
	if a.selectingFile != "" || a.busy || a.closing {
		return
	}
	a.selectingFile = id
	requestID, generation := a.workspace.Draft.ID, a.generation
	picker := a.pickUploadFile
	opts := mygo.OpenDialogOptions{Parent: a.win, Title: "选择上传文件", ButtonLabel: "选择"}
	go func() {
		paths, err := picker(opts)
		a.post(func() {
			a.selectingFile = ""
			if a.closed || a.closing || a.busy || a.workspace.Draft.ID != requestID || a.generation != generation || a.workspace.Draft.BodyMode != core.BodyMultipart {
				return
			}
			if err != nil {
				a.notice = "无法选择文件：" + err.Error()
				return
			}
			if len(paths) == 0 {
				return
			}
			for i := range a.workspace.Draft.FormParts {
				part := &a.workspace.Draft.FormParts[i]
				if part.ID == id && part.Kind == core.FormFile {
					part.FilePath = paths[0]
					a.notice = ""
					a.persist()
					return
				}
			}
		})
	}()
}

func (a *application) bodyView(c *ui.Context) {
	r := &a.workspace.Draft
	mode := "原始文本 / JSON"
	if r.BodyMode == core.BodyMultipart {
		mode = "multipart/form-data"
	}
	ui.Select(c.Key("body-mode"), &mode, []string{"原始文本 / JSON", "multipart/form-data"}).Width(210).Label("正文类型").Disabled(a.busy || r.Protocol == "WebSocket").OnChange(func() {
		r.BodyMode = core.BodyRaw
		if mode == "multipart/form-data" {
			r.BodyMode = core.BodyMultipart
		}
		a.persist()
	})
	if r.BodyMode != core.BodyMultipart {
		ui.Text(c, "JSON 或原始文本 · 变量在发送时替换").FontSize(11).TextColor(c.Theme().TextMuted)
		ui.TextArea(c.Key("body"), &r.Body).Grow(1).FillWidth().Font("monospace").Label("请求正文").Placeholder("{\n  \"hello\": \"world\"\n}").Disabled(a.busy).OnChange(a.persist)
		return
	}
	ui.Scroll(c.Key("form-parts")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if len(r.FormParts) == 0 {
				ui.Text(c, "添加文本字段或文件字段，填写字段名称后发送。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for i := range r.FormParts {
				part := &r.FormParts[i]
				id, number := part.ID, strconv.Itoa(i+1)
				ui.Row(c.Key(id)).Gap(8).Disabled(a.busy).Children(func() {
					ui.TextInput(c.Key("name"), &part.Name).Width(160).Label("字段名称 " + number).Placeholder("字段名称").OnChange(a.persist)
					if part.Kind == core.FormText {
						ui.TextInput(c.Key("value"), &part.Value).Grow(1).MinWidth(0).Label("字段值 " + number).Placeholder("文本或 {{变量}}").OnChange(a.persist)
					} else {
						name := "未选择文件"
						if part.FilePath != "" {
							name = filepath.Base(part.FilePath)
						}
						ui.Text(c, short(name, 32)).Grow(1).MinWidth(0).Tooltip(part.FilePath)
						ui.Button(c.Key("choose"), "选择文件").Label("选择文件 " + number).Disabled(a.selectingFile != "").OnClick(func() { a.chooseFormFile(id) })
					}
					ui.Button(c.Key("remove"), "移除").Label("移除字段 " + number).OnClick(func() { a.removeFormPart(id) })
				})
			}
		})
	})
	ui.Row(c).Gap(8).Disabled(a.busy).Children(func() {
		ui.Button(c.Key("add-text"), "添加文本字段").OnClick(func() { a.addFormPart(core.FormText) })
		ui.Button(c.Key("add-file"), "添加文件字段").OnClick(func() { a.addFormPart(core.FormFile) })
	})
	ui.Text(c, "Content-Type 自动生成 · 文件在发送时读取，仅保存路径").FontSize(11).TextColor(c.Theme().TextMuted)
}
