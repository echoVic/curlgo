package main

import (
	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func (a *application) authView(c *ui.Context) {
	auth := &a.workspace.Draft.Auth
	options := []struct {
		mode  core.AuthMode
		label string
	}{
		{core.AuthManual, "手动请求头"},
		{core.AuthBearer, "Bearer Token"},
		{core.AuthBasic, "Basic Auth"},
		{core.AuthAPIKey, "API Key"},
	}
	current := "不支持的鉴权类型"
	for _, option := range options {
		if auth.Mode == option.mode {
			current = option.label
		}
	}
	t := c.Theme()
	ui.Row(c).Gap(12).Disabled(a.busy).Children(func() {
		sel := ui.SelectBase(c.Key("auth-mode"), &auth.Mode)
		sel.Trigger.Width(190).Height(32).Padding(6, 10).Radius(t.Radius).
			Background(t.Surface).BorderWidth(1).BorderColor(t.Border).Label("鉴权方式").
			OnChange(func() { a.showCredentials = false; a.persist() }).Children(func() {
			ui.Row(c).FillWidth().Children(func() {
				ui.Text(c, current).Grow(1)
				ui.Text(c, "⌄").TextColor(t.TextMuted)
			})
		})
		sel.Popup(func(panel ui.Element) {
			panel.Padding(4).Radius(t.Radius).Background(t.Surface).BorderWidth(1).BorderColor(t.Border)
			for _, option := range options {
				item := sel.Item(option.mode).Padding(7, 10).Radius(t.Radius).Label(option.label)
				if item.Highlighted() {
					item.Background(t.Accent).TextColor(t.AccentText)
				}
				item.Children(func() { ui.Text(c, option.label) })
			}
		})
		if auth.Mode != core.AuthManual {
			ui.Checkbox(c.Key("show-auth"), &a.showCredentials, "显示凭证")
		}
	})
	if auth.Mode == core.AuthManual {
		ui.Text(c, "在「请求头」或全局 Header 中填写鉴权信息。").FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.Scroll(c.Key("auth-fields")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Padding(12).Radius(t.Radius).Background(t.Surface).Disabled(a.busy).Children(func() {
			switch auth.Mode {
			case core.AuthBearer:
				a.credentialInput(c.Key("bearer-token"), &auth.Token, "Bearer Token 值", "Token 或 {{token}}")
			case core.AuthBasic:
				ui.Row(c).Gap(10).Children(func() {
					ui.TextInput(c.Key("basic-user"), &auth.Username).Grow(1).MinWidth(0).
						Label("Basic 用户名").Placeholder("用户名或 {{username}}").OnChange(a.persist)
					a.credentialInput(c.Key("basic-password"), &auth.Password, "Basic 密码", "密码或 {{password}}").Grow(1).MinWidth(0)
				})
			case core.AuthAPIKey:
				location := 0
				if auth.Location == core.APIKeyQuery {
					location = 1
				}
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "添加到").TextColor(t.TextMuted)
					ui.Segmented(c.Key("api-location"), &location, "Header", "Query").Width(180).OnChange(func() {
						auth.Location = core.APIKeyHeader
						if location == 1 {
							auth.Location = core.APIKeyQuery
						}
						a.persist()
					})
				})
				ui.Row(c).Gap(10).Children(func() {
					ui.TextInput(c.Key("api-name"), &auth.Key).Width(210).
						Label("API Key 名称").Placeholder("名称或 {{key_name}}").OnChange(a.persist)
					a.credentialInput(c.Key("api-value"), &auth.Value, "API Key 值", "值或 {{api_key}}").Grow(1).MinWidth(0)
				})
			}
			hint := "发送时替换变量，并覆盖同名 Authorization 请求头。"
			if auth.Mode == core.AuthAPIKey {
				hint = "发送时替换变量，并覆盖同名 Header 或 Query 参数。"
			}
			ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, "鉴权配置随请求保存在本机，也会包含在集合导出中。").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

func (a *application) credentialInput(c *ui.Context, value *string, label, placeholder string) ui.Element {
	input := ui.TextInput(c, value).FillWidth().Label(label).Placeholder(placeholder).OnChange(a.persist)
	if !a.showCredentials {
		input.Password()
	}
	return input
}
