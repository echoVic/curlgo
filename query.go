package main

import (
	"strconv"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func (a *application) syncQueryParams() {
	active, err := core.ParseQueryParams(a.workspace.Draft.URL)
	a.queryError = err
	if err != nil {
		return
	}
	var params []core.QueryParam
	next := 0
	for _, old := range a.workspace.Draft.QueryParams {
		if old.Disabled || !old.HasRaw && old.Key == "" && old.Value == "" {
			if old.ID == "" {
				old.ID = core.NewID()
			}
			params = append(params, old)
		} else if next < len(active) {
			if old.ID != "" {
				active[next].ID = old.ID
			}
			params = append(params, active[next])
			next++
		}
	}
	a.workspace.Draft.QueryParams = append(params, active[next:]...)
}

func (a *application) queryURLChanged() {
	if a.busy || a.closing {
		return
	}
	a.syncQueryParams()
	a.persist()
}

func (a *application) queryParamsChanged() {
	if a.busy || a.closing || a.queryError != nil {
		return
	}
	r := &a.workspace.Draft
	r.URL = core.WithQueryParams(r.URL, r.QueryParams)
	for i := range r.QueryParams {
		p := &r.QueryParams[i]
		if raw, present := p.Encoded(); present {
			p.Raw, p.HasRaw = raw, true
		}
	}
	a.persist()
}

func (a *application) addQueryParam() {
	if a.busy || a.closing || a.queryError != nil {
		return
	}
	a.workspace.Draft.QueryParams = append(a.workspace.Draft.QueryParams, core.QueryParam{ID: core.NewID()})
	a.queryParamsChanged()
}

func (a *application) removeQueryParam(id string) {
	if a.busy || a.closing || a.queryError != nil {
		return
	}
	for i, p := range a.workspace.Draft.QueryParams {
		if p.ID == id {
			a.workspace.Draft.QueryParams = append(a.workspace.Draft.QueryParams[:i], a.workspace.Draft.QueryParams[i+1:]...)
			a.queryParamsChanged()
			return
		}
	}
}

func (a *application) queryView(c *ui.Context) {
	r := &a.workspace.Draft
	ui.Text(c, "与地址实时同步 · 支持重复键、空值和 {{变量}}").FontSize(11).TextColor(c.Theme().TextMuted)
	if a.queryError != nil {
		ui.Text(c, a.queryError.Error()).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	ui.Scroll(c.Key("query-params")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if len(r.QueryParams) == 0 && a.queryError == nil {
				ui.Text(c, "添加参数，或在请求地址中输入 ?key=value。").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for i := range r.QueryParams {
				p := &r.QueryParams[i]
				id, number := p.ID, strconv.Itoa(i+1)
				enabled := !p.Disabled
				ui.Row(c.Key(id)).Gap(8).Disabled(a.busy || a.queryError != nil).Children(func() {
					ui.Checkbox(c.Key("enabled"), &enabled, "").Label("启用参数 " + number).OnChange(func() {
						p.Disabled = !enabled
						a.queryParamsChanged()
					})
					ui.TextInput(c.Key("key"), &p.Key).Grow(1).MinWidth(100).Label("参数名称 " + number).Placeholder("参数名称").OnChange(a.queryParamsChanged)
					ui.TextInput(c.Key("value"), &p.Value).Grow(2).MinWidth(120).Label("参数值 " + number).Placeholder("值或 {{变量}}").OnChange(a.queryParamsChanged)
					ui.Button(c.Key("remove"), "移除").Label("移除参数 " + number).OnClick(func() { a.removeQueryParam(id) })
				})
			}
		})
	})
	ui.Button(c.Key("add-query"), "添加参数").Disabled(a.busy || a.queryError != nil).OnClick(a.addQueryParam)
}
