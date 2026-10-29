package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

// Response data stays in memory and is never part of the workspace snapshot.
type responseCapture struct {
	requestID                             string
	generation                            uint64
	body                                  []byte
	status                                int
	received, complete, failed, truncated bool
}

func (a *application) canExtractResponse() bool {
	s := a.capturedResponse
	if a.busy || a.closed || a.closing || a.workspace.Draft.Protocol != "HTTP" || !s.complete || s.failed ||
		s.requestID != a.workspace.Draft.ID || s.generation != a.generation || s.status < 200 || s.status >= 300 {
		return false
	}
	for _, r := range a.workspace.Draft.Extractors {
		if !r.Disabled {
			return true
		}
	}
	return false
}

func (a *application) extractCurrentResponse() {
	if a.canExtractResponse() && a.evaluateResponseAssertions(a.workspace.Draft.Assertions) {
		a.applyExtractedVariables(a.workspace.Draft.Extractors, a.workspace.Settings.Environment)
	}
}

func (a *application) applyExtractedVariables(rules []core.ResponseExtractor, environment string) {
	values, err := core.ExtractVariables(rules, a.responseHeaders, a.capturedResponse.body, a.capturedResponse.truncated)
	if err != nil {
		a.extractionNotice = "提取失败：" + err.Error()
		a.notice = a.extractionNotice
		return
	}
	if len(values) == 0 {
		return
	}
	old, exists := a.workspace.Settings.Environments[environment]
	if !exists {
		a.extractionNotice = "提取失败：发送时的环境已不存在"
		a.notice = a.extractionNotice
		return
	}
	private, ordinary := map[string]string{}, map[string]string{}
	for name, value := range values {
		if strings.HasPrefix(name, core.SecretPrefix) {
			private[strings.TrimPrefix(name, core.SecretPrefix)] = value
		} else {
			ordinary[name] = value
		}
	}
	if len(private) > 0 {
		if a.secrets == nil || !a.secrets.Unlocked() {
			a.extractionNotice = "提取失败：请先解锁机密变量"
			a.notice = a.extractionNotice
			return
		}
		if err := a.secrets.SetBatch(environment, private); err != nil {
			a.extractionNotice = "提取失败：机密变量未保存，环境变量未更新"
			a.notice = a.extractionNotice
			return
		}
	}
	merged := make(map[string]string, len(old)+len(values))
	for k, v := range old {
		merged[k] = v
	}
	for k, v := range ordinary {
		merged[k] = v
	}
	a.workspace.Settings.Environments[environment] = merged
	if strings.TrimSpace(a.envName) == environment {
		data, _ := json.MarshalIndent(merged, "", "  ")
		a.variables = string(data)
	}
	a.notice = ""
	a.extractionNotice = fmt.Sprintf("已更新环境「%s」中的 %d 个变量", environment, len(values))
	a.toast = a.extractionNotice
	a.persist()
}

func (a *application) addExtractor() {
	if a.busy || a.closed || a.closing || len(a.workspace.Draft.Extractors) >= core.MaxExtractors {
		return
	}
	a.workspace.Draft.Extractors = append(a.workspace.Draft.Extractors, core.ResponseExtractor{ID: core.NewID(), Source: core.ExtractJSON})
	a.extractionNotice = ""
	a.persist()
}

func (a *application) removeExtractor(id string) {
	if a.busy || a.closed || a.closing {
		return
	}
	for i, r := range a.workspace.Draft.Extractors {
		if r.ID == id {
			a.workspace.Draft.Extractors = append(a.workspace.Draft.Extractors[:i], a.workspace.Draft.Extractors[i+1:]...)
			a.extractionNotice = ""
			a.persist()
			return
		}
	}
}

func (a *application) extractionView(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "HTTP 2xx 且断言通过后写入发送时的环境 · secret.token 写入机密变量").FontSize(11).TextColor(t.TextMuted)
	ui.Scroll(c.Key("extraction-rules")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Disabled(a.busy).Children(func() {
			if len(a.workspace.Draft.Extractors) == 0 {
				ui.Text(c, "用 JSON Pointer（如 /data/access_token）或响应头提取变量。").FontSize(12).TextColor(t.TextMuted)
			}
			for i := range a.workspace.Draft.Extractors {
				r := &a.workspace.Draft.Extractors[i]
				id, number := r.ID, strconv.Itoa(i+1)
				enabled := !r.Disabled
				ui.Column(c.Key(id)).FillWidth().Gap(6).Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.Checkbox(c.Key("enabled"), &enabled, "").Label("启用提取规则 " + number).OnChange(func() {
							r.Disabled = !enabled
							a.persist()
						})
						ui.TextInput(c.Key("variable"), &r.Variable).Grow(1).MinWidth(100).
							Label("目标变量 " + number).Placeholder("变量名，如 id 或 secret.token").OnChange(a.persist)
						source := "JSON 正文"
						if r.Source == core.ExtractHeader {
							source = "响应头"
						}
						sel := ui.SelectBase(c.Key("source"), &r.Source)
						sel.Trigger.Width(112).Height(30).Padding(5, 8).Radius(t.Radius).
							Background(t.Surface).BorderWidth(1).BorderColor(t.Border).Label("提取来源 " + number).
							OnChange(a.persist).Children(func() {
							ui.Row(c).FillWidth().Children(func() {
								ui.Text(c, source).Grow(1)
								ui.Text(c, "⌄").TextColor(t.TextMuted)
							})
						})
						sel.Popup(func(panel ui.Element) {
							panel.Padding(4).Radius(t.Radius).Background(t.Surface).BorderWidth(1).BorderColor(t.Border)
							for _, option := range []struct {
								source core.ExtractionSource
								label  string
							}{{core.ExtractJSON, "JSON 正文"}, {core.ExtractHeader, "响应头"}} {
								item := sel.Item(option.source).Padding(7, 10).Radius(t.Radius).Label("提取" + option.label)
								if item.Highlighted() {
									item.Background(t.Accent).TextColor(t.AccentText)
								}
								item.Children(func() { ui.Text(c, option.label) })
							}
						})
						ui.Button(c.Key("remove"), "移除").Label("移除提取规则 " + number).OnClick(func() { a.removeExtractor(id) })
					})
					placeholder := "/data/access_token · 空路径选择根值"
					if r.Source == core.ExtractHeader {
						placeholder = "响应头名称，如 X-Request-Id"
					}
					ui.TextInput(c.Key("selector"), &r.Selector).FillWidth().Label("选择器 " + number).
						Placeholder(placeholder).OnChange(a.persist)
				})
			}
			if a.extractionNotice != "" {
				ui.Text(c, a.extractionNotice).FontSize(11).TextColor(t.TextMuted).FillWidth()
			}
		})
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Button(c.Key("add-extractor"), "添加提取规则").
			Disabled(a.busy || len(a.workspace.Draft.Extractors) >= core.MaxExtractors).OnClick(a.addExtractor)
		ui.Button(c.Key("manual-extraction"), "从当前响应提取").Disabled(!a.canExtractResponse()).OnClick(a.extractCurrentResponse)
	})
}
