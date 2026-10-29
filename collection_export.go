package main

import (
	"fmt"
	"unicode"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type collectionExportPreview struct {
	options  core.CollectionExportOptions
	revision uint64
	valid    bool
	result   core.CollectionExportResult
	err      error
}

func (a *application) openCollectionExport() {
	opts := core.CollectionExportOptions{Name: "CurlGo", Format: core.CollectionCurlGo}
	for _, g := range a.workspace.Groups {
		if g.ID == a.groupFilter {
			opts.GroupID, opts.Name = g.ID, g.Name
		}
	}
	a.exportOptions = opts
	a.exportPreview.valid = false
	a.page, a.notice = 5, ""
}

func collectionFilename(name string) string {
	var out []rune
	for _, r := range name {
		if len(out) >= 100 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "CurlGo.json"
	}
	return string(out) + ".json"
}

func (a *application) refreshExportPreview() {
	if a.exportPreview.valid && a.exportPreview.options == a.exportOptions && a.exportPreview.revision == a.revision {
		return
	}
	result, err := core.ExportCollection(a.workspace, a.exportOptions)
	a.exportPreview = collectionExportPreview{
		options: a.exportOptions, revision: a.revision, valid: true, result: result, err: err,
	}
}

func (a *application) chooseCollectionExport() {
	if a.exportingCollection || a.closing || a.closed {
		return
	}
	// Encode before opening the dialog. The worker only owns these bytes and
	// cannot observe later edits to requests, settings, or the chosen scope.
	result, err := core.ExportCollection(a.workspace, a.exportOptions)
	if err != nil {
		a.notice = err.Error()
		return
	}
	opts := mygo.SaveDialogOptions{
		Parent: a.win, Title: "导出集合", ButtonLabel: "保存",
		DefaultPath: collectionFilename(a.exportOptions.Name), CreateDirectories: true,
		Filters: []mygo.FileFilter{{Name: "集合 JSON", Extensions: []string{"json"}}},
	}
	a.exportingCollection, a.notice = true, ""
	picker := a.pickExportFile
	go func() {
		path, err := picker(opts)
		if err == nil && path != "" {
			err = core.SaveCollection(path, result.Data)
		}
		a.post(func() {
			a.exportingCollection = false
			if a.closed || a.closing {
				return
			}
			if err != nil {
				a.notice = "集合保存失败：" + err.Error()
				return
			}
			if path != "" {
				a.toast = fmt.Sprintf("已导出 %d 个请求", result.RequestCount)
			}
		})
	}()
}

func (a *application) collectionExportView(c *ui.Context) {
	t := c.Theme()
	opts := &a.exportOptions
	ui.Row(c).Children(func() {
		ui.Text(c, "导出收藏集合").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("back-export"), "返回请求").OnClick(func() { a.page = 0 })
	})
	ui.Text(c, "保存请求模板与分组，便于备份或交给其他 API 工具。").TextColor(t.TextMuted)
	ui.TextInput(c.Key("export-name"), &opts.Name).Label("集合名称").FillWidth().Placeholder("集合名称")
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, "范围").TextColor(t.TextMuted)
		groupSelect(c.Key("export-scope"), &opts.GroupID, a.groupOptions("全部收藏"), "导出范围", nil).Grow(1)
	})
	format := "CurlGo JSON"
	if opts.Format == core.CollectionPostman {
		format = "Postman v2.1"
	}
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, "格式").TextColor(t.TextMuted)
		ui.Select(c.Key("export-format"), &format, []string{"CurlGo JSON", "Postman v2.1"}).
			Label("导出格式").Width(220).OnChange(func() {
			opts.Format = core.CollectionCurlGo
			if format == "Postman v2.1" {
				opts.Format = core.CollectionPostman
			}
		})
	})
	ui.Checkbox(c.Key("export-environment"), &opts.IncludeEnvironment, "包含环境与全局请求头")
	ui.Text(c, "环境值和全局 Header 默认不包含；请求中的 Header、正文与鉴权配置会保留。").FontSize(11).TextColor(t.TextMuted)
	a.refreshExportPreview()
	preview := a.exportPreview
	ui.Scroll(c.Key("export-details")).Grow(1).MinHeight(60).Children(func() {
		ui.Column(c).Gap(10).FillWidth().Children(func() {
			if preview.err != nil {
				ui.Text(c, preview.err.Error()).TextColor(t.TextMuted)
			} else {
				ui.Text(c, fmt.Sprintf("将导出 %d 个请求", preview.result.RequestCount)).Bold()
			}
			if opts.Format == core.CollectionPostman {
				ui.Text(c, "Postman v2.1 仅导出 HTTP。SSE / WebSocket 会跳过；不保留自定义超时和禁用 Query 行的编辑状态。").FontSize(12)
			} else {
				ui.Text(c, "CurlGo JSON 可完整回导分组、HTTP / SSE / WebSocket、Query 参数、正文、超时与重定向设置。").FontSize(12)
			}
			ui.Text(c, "变量模板保持原样；文件字段只保存路径。Cookie、历史、响应、代理与 CA 不包含在集合中。").FontSize(11).TextColor(t.TextMuted)
			for _, warning := range preview.result.Warnings {
				ui.Text(c, "· "+warning).FontSize(11)
			}
		})
	})
	ui.PrimaryButton(c.Key("save-export"), "保存集合文件").Disabled(a.exportingCollection || preview.err != nil).OnClick(a.chooseCollectionExport)
}
