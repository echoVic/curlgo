package main

import (
	"fmt"
	"strconv"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func (a *application) finishHTTPResponse(r core.Request, environment string, generation uint64) {
	s := &a.capturedResponse
	if a.closed || a.closing || generation != a.generation || r.ID != a.workspace.Draft.ID ||
		s.generation != generation || !s.received || s.failed {
		return
	}
	s.complete = true
	if !a.evaluateResponseAssertions(r.Assertions) {
		return
	}
	if s.status >= 200 && s.status < 300 {
		a.applyExtractedVariables(r.Extractors, environment)
	}
}

func (a *application) canValidateResponse() bool {
	s := a.capturedResponse
	return !a.busy && !a.closed && !a.closing && a.workspace.Draft.Protocol == "HTTP" && s.complete && !s.failed &&
		s.requestID == a.workspace.Draft.ID && s.generation == a.generation
}

func (a *application) validateCurrentResponse() {
	if a.canValidateResponse() {
		a.evaluateResponseAssertions(a.workspace.Draft.Assertions)
	}
}

func (a *application) resetAssertionResult() {
	if a.notice == a.assertionNotice {
		a.notice = ""
	}
	a.assertionReport = nil
	a.assertionNotice = ""
}

func (a *application) evaluateResponseAssertions(rules []core.ResponseAssertion) bool {
	a.resetAssertionResult()
	s := a.capturedResponse
	report, err := core.EvaluateAssertions(rules, s.status, a.responseHeaders, s.body, s.truncated)
	if err != nil {
		a.assertionNotice = err.Error()
		a.notice = a.assertionNotice
		return false
	}
	if len(report.Results) == 0 {
		return true
	}
	a.assertionReport = &report
	passed := 0
	for _, r := range report.Results {
		if r.Passed {
			passed++
		}
	}
	a.assertionNotice = fmt.Sprintf("断言通过 %d / %d", passed, len(report.Results))
	if !report.Passed {
		a.assertionNotice = fmt.Sprintf("断言失败：%d / %d 通过；未更新响应变量", passed, len(report.Results))
		a.notice = a.assertionNotice
	}
	return report.Passed
}

func (a *application) assertionsChanged() {
	a.resetAssertionResult()
	a.persist()
}

func (a *application) addAssertion() {
	if a.busy || a.closed || a.closing || len(a.workspace.Draft.Assertions) >= core.MaxAssertions {
		return
	}
	a.workspace.Draft.Assertions = append(a.workspace.Draft.Assertions, core.ResponseAssertion{
		ID: core.NewID(), Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200",
	})
	a.assertionsChanged()
}

func (a *application) removeAssertion(id string) {
	if a.busy || a.closed || a.closing {
		return
	}
	for i, r := range a.workspace.Draft.Assertions {
		if r.ID == id {
			a.workspace.Draft.Assertions = append(a.workspace.Draft.Assertions[:i], a.workspace.Draft.Assertions[i+1:]...)
			a.assertionsChanged()
			return
		}
	}
}

func (a *application) assertionsView(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "验证完整 HTTP 响应 · 全部通过后才提取变量 · 期望值按字面比较").FontSize(11).TextColor(t.TextMuted)
	ui.Scroll(c.Key("assertion-rules")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Disabled(a.busy).Children(func() {
			if len(a.workspace.Draft.Assertions) == 0 {
				ui.Text(c, "添加状态码、响应头、JSON 字段或正文断言。").FontSize(12).TextColor(t.TextMuted)
			}
			for i := range a.workspace.Draft.Assertions {
				r := &a.workspace.Draft.Assertions[i]
				id, number := r.ID, strconv.Itoa(i+1)
				enabled := !r.Disabled
				opacity := float32(1)
				if r.Disabled {
					opacity = 0.5
				}
				ui.Column(c.Key(id)).FillWidth().Gap(6).Padding(10).Radius(t.Radius).Background(t.Surface).Opacity(opacity).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Checkbox(c.Key("enabled"), &enabled, "").Label("启用断言 " + number).OnChange(func() {
							r.Disabled = !enabled
							a.assertionsChanged()
						})
						ui.TextInput(c.Key("name"), &r.Name).Grow(1).MinWidth(80).Label("断言名称 " + number).
							Placeholder("名称（可选）").OnChange(a.assertionsChanged)
						a.assertionSourceSelect(c.Key("source"), r, number)
						a.assertionOperatorSelect(c.Key("operator"), r, number)
						ui.Button(c.Key("remove"), "移除").Label("移除断言 " + number).OnClick(func() { a.removeAssertion(id) })
					})
					ui.Row(c).Gap(6).Children(func() {
						if r.Source == core.AssertJSON || r.Source == core.AssertHeader {
							hint := "/data/ok · 空路径为根值"
							if r.Source == core.AssertHeader {
								hint = "响应头名称，如 X-Trace"
							}
							ui.TextInput(c.Key("selector"), &r.Selector).Grow(1).MinWidth(100).Label("断言选择器 " + number).
								Placeholder(hint).OnChange(a.assertionsChanged)
						}
						if r.Operator != core.AssertExists {
							hint := "期望文本"
							if r.Source == core.AssertStatus {
								hint = "状态码，如 200"
							} else if r.Source == core.AssertJSON {
								hint = `JSON 字面量："ok" / 200 / true / null`
							}
							ui.TextInput(c.Key("expected"), &r.Expected).Grow(1).MinWidth(100).Label("断言期望值 " + number).
								Placeholder(hint).OnChange(a.assertionsChanged)
						}
					})
				})
			}
			if a.assertionNotice != "" {
				ui.Text(c, a.assertionNotice).FontSize(11).TextColor(t.TextMuted).FillWidth()
			}
		})
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Button(c.Key("add-assertion"), "＋ 添加断言").
			Disabled(a.busy || len(a.workspace.Draft.Assertions) >= core.MaxAssertions).OnClick(a.addAssertion)
		ui.Button(c.Key("validate-response"), "验证当前响应").Disabled(!a.canValidateResponse()).OnClick(a.validateCurrentResponse)
	})
}

func (a *application) assertionSourceSelect(c *ui.Context, r *core.ResponseAssertion, number string) {
	t := c.Theme()
	options := []struct {
		value core.AssertionSource
		label string
	}{{core.AssertStatus, "状态码"}, {core.AssertHeader, "响应头"}, {core.AssertJSON, "JSON 字段"}, {core.AssertBody, "正文"}}
	label := "选择来源"
	for _, option := range options {
		if r.Source == option.value {
			label = option.label
		}
	}
	sel := ui.SelectBase(c, &r.Source)
	sel.Trigger.Width(104).Height(30).Padding(5, 8).Radius(t.Radius).Background(t.Background).
		BorderWidth(1).BorderColor(t.Border).Label("断言来源 " + number).OnChange(func() {
		r.Selector, r.Expected, r.Operator = "", "", core.AssertEquals
		switch r.Source {
		case core.AssertStatus:
			r.Expected = "200"
		case core.AssertJSON:
			r.Expected = `""`
		}
		a.assertionsChanged()
	}).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, label).Grow(1)
			ui.Text(c, "⌄").TextColor(t.TextMuted)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Radius).Background(t.Surface).BorderWidth(1).BorderColor(t.Border)
		for _, option := range options {
			item := sel.Item(option.value).Padding(7, 10).Radius(t.Radius).Label("断言 " + option.label)
			if item.Highlighted() {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			item.Children(func() { ui.Text(c, option.label) })
		}
	})
}

func (a *application) assertionOperatorSelect(c *ui.Context, r *core.ResponseAssertion, number string) {
	t := c.Theme()
	options := []struct {
		value core.AssertionOperator
		label string
	}{{core.AssertEquals, "相等"}}
	if r.Source != core.AssertStatus {
		options = append(options, struct {
			value core.AssertionOperator
			label string
		}{core.AssertContains, "包含"})
	}
	if r.Source == core.AssertJSON || r.Source == core.AssertHeader {
		options = append(options, struct {
			value core.AssertionOperator
			label string
		}{core.AssertExists, "存在"})
	}
	label := "选择操作"
	for _, option := range options {
		if r.Operator == option.value {
			label = option.label
		}
	}
	sel := ui.SelectBase(c, &r.Operator)
	sel.Trigger.Width(86).Height(30).Padding(5, 8).Radius(t.Radius).Background(t.Background).
		BorderWidth(1).BorderColor(t.Border).Label("断言操作 " + number).OnChange(a.assertionsChanged).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, label).Grow(1)
			ui.Text(c, "⌄").TextColor(t.TextMuted)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Radius).Background(t.Surface).BorderWidth(1).BorderColor(t.Border)
		for _, option := range options {
			item := sel.Item(option.value).Padding(7, 10).Radius(t.Radius).Label("断言" + option.label)
			if item.Highlighted() {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			item.Children(func() { ui.Text(c, option.label) })
		}
	})
}

func (a *application) assertionResultsView(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c.Key("assertion-results")).Grow(1).FillWidth().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if a.assertionReport == nil {
				ui.Text(c, "发送请求后查看断言结果；编辑规则后可验证当前响应。").FontSize(12).TextColor(t.TextMuted)
				return
			}
			summaryColor := t.Success
			if !a.assertionReport.Passed {
				summaryColor = t.Danger
			}
			ui.Text(c, a.assertionNotice).Bold().FontSize(12).TextColor(summaryColor)
			for _, result := range a.assertionReport.Results {
				label := result.Name
				if label == "" {
					label = "断言 " + strconv.Itoa(result.Index+1)
				}
				indicator := "✓"
				indicatorColor := t.Success
				if !result.Passed {
					indicator = "✕"
					indicatorColor = t.Danger
				}
				ui.Row(c.Key(strconv.Itoa(result.Index))).FillWidth().Gap(10).Children(func() {
					ui.Text(c, indicator).FontSize(13).Bold().TextColor(indicatorColor)
					ui.Text(c, label).FontSize(12).Grow(1).TextColor(t.TextMuted)
					ui.Text(c, result.Message).FontSize(12).Label(result.Message).TextColor(t.Text)
				})
			}
		})
	})
}
