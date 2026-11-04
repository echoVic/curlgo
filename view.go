package main

import (
	"sort"
	"strconv"
	"strings"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func (a *application) view(c *ui.Context) {
	if a.page != 6 && a.secretPageActive {
		a.leaveSecretPage()
	}
	a.secretPageActive = a.page == 6
	t := ui.LightTheme()
	if a.workspace.Settings.Theme == "深色" {
		t = ui.DarkTheme()
		// 紫罗兰优雅 · 深色：靛蓝主色 + 冷紫灰中性色
		t.Background = ui.Hex("#0f0f1a")
		t.Surface = ui.Hex("#181826")
		t.SurfaceHover = ui.Hex("#22223a")
		t.SurfacePressed = ui.Hex("#2a2a45")
		t.Border = ui.Hex("#2a2a3e")
		t.Text = ui.Hex("#e4e6ed")
		t.TextMuted = ui.Hex("#8b8fa3")
		t.Accent = ui.Hex("#6366f1")
		t.AccentHover = ui.Hex("#818cf8")
		t.AccentPressed = ui.Hex("#4f46e5")
		t.AccentText = ui.Hex("#ffffff")
		t.Danger = ui.Hex("#f87171")
		t.Warning = ui.Hex("#fbbf24")
		t.Success = ui.Hex("#34d399")
		t.Selection = ui.RGBA(99, 102, 241, 0.30)
		t.Focus = ui.RGBA(129, 140, 248, 0.55)
		t.Scrollbar = ui.RGBA(139, 143, 163, 0.35)
		t.ScrollbarWidth = 8
		t.Inverse = ui.Hex("#1a1a2e")
		t.InverseText = ui.Hex("#e4e6ed")
	} else {
		// 紫罗兰优雅 · 浅色
		t.Background = ui.Hex("#f5f5fa")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#eeeef7")
		t.SurfacePressed = ui.Hex("#e4e4f0")
		t.Border = ui.Hex("#e0e0ee")
		t.Text = ui.Hex("#1a1a2e")
		t.TextMuted = ui.Hex("#6b6b80")
		t.Accent = ui.Hex("#6366f1")
		t.AccentHover = ui.Hex("#4f46e5")
		t.AccentPressed = ui.Hex("#4338ca")
		t.AccentText = ui.Hex("#ffffff")
		t.Danger = ui.Hex("#dc2626")
		t.Warning = ui.Hex("#d97706")
		t.Success = ui.Hex("#059669")
		t.Selection = ui.RGBA(99, 102, 241, 0.20)
		t.Focus = ui.RGBA(79, 70, 229, 0.45)
		t.Scrollbar = ui.RGBA(107, 107, 128, 0.30)
		t.ScrollbarWidth = 8
		t.Inverse = ui.Hex("#1a1a2e")
		t.InverseText = ui.Hex("#f5f5fa")
	}
	t.Radius = 8
	t.FontSize = 13
	c.SetTheme(t)
	if !a.closing {
		c.OnShortcut(ui.Cmd, ui.KeyN, a.newRequest)
		c.OnShortcut(ui.Cmd, ui.KeyS, a.saveRequest)
		c.OnShortcut(ui.Cmd, ui.KeyEnter, a.send)
		c.OnShortcut(ui.Cmd, ui.KeyL, func() { a.page = 0; a.urlHandle.Focus(c) })
		c.OnShortcut(0, ui.KeyEscape, a.stop)
	}
	ui.Column(c).Fill().Background(t.Background).Disabled(a.closing).Children(func() {
		ui.Row(c).Height(58).Padding(14, 22).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
			ui.Image(c, brandIcon).Size(32, 32)
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, "CurlGo").FontSize(16).Bold()
				ui.Text(c, "贴入请求，马上开始").TextColor(t.TextMuted).FontSize(11)
			})
			ui.Spacer(c)
			ui.Box(c).Padding(4, 10).Radius(999).Background(t.SurfaceHover).Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(999).Background(t.Success)
					ui.Text(c, "本地工作区").TextColor(t.TextMuted).FontSize(11)
				})
			})
			ui.Select(c.Key("theme"), &a.workspace.Settings.Theme, []string{"浅色", "深色"}).Width(78).Label("主题").OnChange(a.persist)
		})
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c)
			ui.Column(c).Grow(1).MinWidth(0).Padding(22).Gap(14).Children(func() {
				if a.page == 1 {
					a.settingsView(c)
				} else if a.page == 2 {
					a.importView(c)
				} else if a.page == 3 && a.collectionImport != nil {
					a.collectionView(c)
				} else if a.page == 4 {
					a.groupsView(c)
				} else if a.page == 5 {
					a.collectionExportView(c)
				} else if a.page == 6 {
					a.secretsView(c)
				} else if a.page == 7 {
					a.appSettingsView(c)
				} else {
					a.requestView(c)
				}
			})
		})
		if a.notice != "" {
			ui.Row(c).Padding(10, 22).Gap(12).Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Accent).Children(func() {
				ui.Text(c, a.notice).Grow(1).FontSize(12)
				ui.Button(c.Key("dismiss"), "知道了").OnClick(func() { a.notice = "" })
			})
		}
		ui.Row(c).Height(28).PaddingX(16).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Text(c, a.saveStatus).TextColor(t.TextMuted).FontSize(11)
			ui.Spacer(c)
			ui.Text(c, "⌘ N 新请求     ⌘ L 地址     ⌘ ↵ 发送     Esc 停止").TextColor(t.TextMuted).FontSize(11)
		})
	})
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
}
func (a *application) sidebar(c *ui.Context) {
	t := c.Theme()
	paths := core.GroupPaths(a.workspace.Groups)
	ui.Column(c).Width(230).Shrink(0).Padding(16, 12).Gap(12).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.PrimaryButton(c.Key("new"), "＋  新请求").FillWidth().Height(36).OnClick(a.newRequest)
		ui.Button(c.Key("import"), "导入 curl / 集合").FillWidth().OnClick(func() { a.page = 2; a.notice = "" })
		ui.SearchField(c.Key("search"), &a.search).Label("搜索请求")
		ui.Segmented(c.Key("library"), &a.libraryTab, "收藏", "历史").FillWidth()
		var selectedGroups map[string]bool
		if a.libraryTab == 0 {
			options := a.groupOptions("全部收藏")
			options = append(options[:1], append([]groupOption{{ungroupedFilter, "未分组"}}, options[1:]...)...)
			groupSelect(c.Key("group-filter"), &a.groupFilter, options, "筛选分组", nil).FillWidth()
			ui.Row(c).Gap(6).Children(func() {
				ui.Button(c.Key("manage-groups"), "管理分组").Grow(1).OnClick(a.openGroups)
				ui.Button(c.Key("export-collection"), "导出集合").Grow(1).OnClick(a.openCollectionExport)
			})
			if a.groupFilter != "" && a.groupFilter != ungroupedFilter {
				selectedGroups = core.GroupSubtree(a.workspace.Groups, a.groupFilter)
			}
		}
		var rows []struct {
			r    core.Request
			time string
		}
		if a.libraryTab == 0 {
			for _, r := range a.workspace.Requests {
				if a.groupFilter == ungroupedFilter && paths[r.GroupID] != "" || selectedGroups != nil && !selectedGroups[r.GroupID] {
					continue
				}
				if a.matches(r, paths[r.GroupID]) {
					rows = append(rows, struct {
						r    core.Request
						time string
					}{r, ""})
				}
			}
		} else {
			for _, h := range a.workspace.History {
				if a.matches(h.Request, paths[h.Request.GroupID]) {
					rows = append(rows, struct {
						r    core.Request
						time string
					}{h.Request, h.At.Format("15:04")})
				}
			}
		}
		a.list.Key = func(i int) any {
			if i >= 0 && i < len(rows) {
				return rows[i].r.ID
			}
			return i
		}
		ui.List(c.Key("requests"), &a.list, len(rows), func(i int) {
			item := rows[i]
			r := item.r
			mc := methodColor(r.Method)
			if a.libraryTab == 0 {
				ui.Column(c).PaddingY(2).Gap(2).Children(func() {
					ui.ButtonBase(c.Key("pick")).FillWidth().Height(34).Padding(6, 8).Radius(t.Radius).Label("打开 " + r.Name).
						OnClick(func() { a.selectRequest(r) }).Children(func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Padding(1, 6).Radius(4).Background(mc).Children(func() {
								ui.Text(c, r.Method).FontSize(10).Bold().TextColor(ui.Hex("#ffffff"))
							})
							ui.Text(c, short(r.Name, 18)).FontSize(12).Grow(1)
						})
					})
					ui.Row(c).PaddingX(8).Children(func() {
						ui.Text(c, short(r.URL, 26)).FontSize(10).TextColor(t.TextMuted).Grow(1)
						ui.Button(c.Key("remove"), "移除").FontSize(10).OnClick(func() { a.deleteRequest(r.ID) })
					})
				})
			} else {
				ui.ButtonBase(c.Key("pick")).FillWidth().Height(34).Padding(6, 8).Radius(t.Radius).Label("打开 " + r.URL).
					OnClick(func() { a.selectRequest(r) }).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, item.time).FontSize(10).TextColor(t.TextMuted)
						ui.Box(c).Padding(1, 6).Radius(4).Background(mc).Children(func() {
							ui.Text(c, r.Method).FontSize(10).Bold().TextColor(ui.Hex("#ffffff"))
						})
						ui.Text(c, short(r.URL, 20)).FontSize(12).Grow(1)
					})
				})
			}
		}).Grow(1).MinHeight(0).Children(func() {
			if len(rows) == 0 {
				if a.libraryTab == 0 {
					if len(a.workspace.Requests) == 0 {
						ui.Column(c).Padding(12, 4).Gap(8).Children(func() {
							ui.Text(c, "常用请求，留在手边").TextColor(t.TextMuted)
							ui.Text(c, "填好地址后，点「收藏」即可。\n临时请求可以直接发送。").TextColor(t.TextMuted).FontSize(11)
						})
					} else {
						ui.Text(c, "没有匹配的请求").TextColor(t.TextMuted).FontSize(11).Padding(12, 4)
					}
				} else {
					ui.Text(c, "发出的请求会出现在这里").TextColor(t.TextMuted).FontSize(11).Padding(12, 4)
				}
			}
		})
		if a.libraryTab == 1 && len(a.workspace.History) > 0 {
			ui.Button(c.Key("clear-history"), "清空历史").OnClick(func() { a.workspace.History = nil; a.persist() })
		}
		ui.Divider(c)
		ui.Button(c.Key("secrets"), "机密变量").FillWidth().OnClick(func() { a.page = 6; a.notice = "" })
		ui.Button(c.Key("settings"), "环境与全局请求头").FillWidth().OnClick(func() { a.syncSettings(); a.page = 1; a.notice = "" })
		ui.Button(c.Key("app-settings"), "设置").FillWidth().OnClick(func() { a.page = 7; a.notice = "" })
		ui.Text(c, "不登录 · 不上传 · 随时离开").TextColor(t.TextMuted).FontSize(10)
	})
}
func (a *application) matches(r core.Request, path string) bool {
	if a.search == "" {
		return true
	}
	return strings.Contains(strings.ToLower(r.Name+" "+r.URL+" "+path), strings.ToLower(a.search))
}
func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// methodColor returns a semantic color for an HTTP method, matching common
// API tool conventions so the sidebar list is scannable at a glance.
func methodColor(method string) ui.Color {
	switch method {
	case "GET":
		return ui.Hex("#10b981")
	case "POST":
		return ui.Hex("#3b82f6")
	case "PUT":
		return ui.Hex("#f59e0b")
	case "PATCH":
		return ui.Hex("#a855f7")
	case "DELETE":
		return ui.Hex("#ef4444")
	default:
		return ui.Hex("#6b7280")
	}
}

// parseStatusPrefix extracts the leading HTTP status code from a status
// string like "200 OK  ·  12 ms". Returns ok=false for non-HTTP statuses.
func parseStatusPrefix(status string) (int, bool) {
	if len(status) < 3 {
		return 0, false
	}
	code, err := strconv.Atoi(status[:3])
	if err != nil || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

func (a *application) requestView(c *ui.Context) {
	t := c.Theme()
	r := &a.workspace.Draft
	services := c.Services()
	ui.Row(c).Gap(12).Children(func() {
		ui.TextInput(c.Key("name"), &r.Name).Grow(1).Label("请求名称").OnChange(a.persist)
		ui.Button(c.Key("save"), "收藏  ⌘ S").OnClick(a.saveRequest)
		ui.Button(c.Key("copy-curl"), "复制 curl").Disabled(r.Protocol == "WebSocket").OnClick(func() {
			if !a.validateDraft() {
				return
			}
			cmd, err := core.ExportCURL(a.workspace.Draft, a.workspace.Settings)
			if err != nil {
				a.notice = err.Error()
				return
			}
			services.WriteClipboard(cmd)
			a.toast = "curl 已复制（普通变量已替换，机密变量保留引用）"
		})
	})
	ui.Row(c).Gap(10).Children(func() {
		ui.Select(c.Key("protocol"), &r.Protocol, []string{"HTTP", "SSE", "WebSocket"}).Width(115).Label("协议").Disabled(a.busy).OnChange(func() { a.response = ""; a.stream = ""; a.persist() })
		var envs []string
		for k := range a.workspace.Settings.Environments {
			envs = append(envs, k)
		}
		sort.Strings(envs)
		ui.Text(c, "环境").TextColor(t.TextMuted)
		ui.Select(c.Key("environment"), &a.workspace.Settings.Environment, envs).Width(160).Label("当前环境").Disabled(a.busy).OnChange(a.persist)
		ui.Spacer(c)
		ui.Text(c, "{{name}} 引用环境变量").FontSize(11).TextColor(t.TextMuted)
	})
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, "分组").TextColor(t.TextMuted)
		groupSelect(c.Key("request-group"), &r.GroupID, a.groupOptions("未分组"), "所属分组", a.moveDraftGroup).
			Grow(1).Disabled(a.busy)
		ui.Button(c.Key("duplicate-request"), "复制收藏").Disabled(a.busy || !a.savedDraft()).
			OnClick(func() { a.copySavedRequest(a.workspace.Draft.ID) })
	})
	ui.Row(c).Padding(6).Gap(8).Radius(12).Background(t.Surface).BorderWidth(1).BorderColor(t.Border).Children(func() {
		ui.Select(c.Key("method"), &r.Method, []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}).Width(100).Label("请求方法").Disabled(a.busy || r.Protocol == "WebSocket").OnChange(a.persist)
		ui.TextInput(c.Key("url"), &r.URL).Bind(&a.urlHandle).Grow(1).Height(36).Placeholder("https://… 或 {{base_url}}/api").Label("请求地址").Disabled(a.busy).OnChange(a.queryURLChanged).OnSubmit(a.send)
		label := "发送  ⌘ ↵"
		if r.Protocol == "WebSocket" {
			label = "连接"
		}
		if a.busy {
			label = "停止"
		}
		ui.PrimaryButton(c.Key("send"), label).Width(112).Height(36).OnClick(a.send)
	})
	splitSize, splitView := &a.split, ui.SplitVertical
	if a.requestTab == 7 {
		splitSize, splitView = &a.assertionSplit, ui.SplitVerticalPercent
	}
	splitView(c.Key("panes"), splitSize, func() {
		ui.Column(c).Fill().Gap(10).Padding(0, 0, 8, 0).Children(func() {
			ui.Tabs(c.Key("request-tabs"), &a.requestTab, "请求头", "正文", "Cookie", "选项", "参数", "鉴权", "提取", "断言")
			switch a.requestTab {
			case 0:
				ui.Text(c, "单个请求的同名 Header 会覆盖全局 Header").FontSize(11).TextColor(t.TextMuted)
				ui.TextArea(c.Key("headers"), &a.headers).Grow(1).FillWidth().Font("monospace").Label("请求头内容").Placeholder("Content-Type: application/json\nAuthorization: Bearer {{token}}").Disabled(a.busy).OnChange(func() {
					h, err := core.ParseHeaders(a.headers)
					if err != nil {
						a.notice = err.Error()
						return
					}
					r.Headers = h
					a.persist()
				})
			case 1:
				a.bodyView(c)
			case 2:
				resolved, err := core.ResolveWithSecrets(*r, a.workspace.Settings, a.secretValues(a.workspace.Settings.Environment))
				raw := resolved.URL
				if err != nil {
					raw = ""
				}
				cookies := a.engine.Cookies(raw)
				ui.Text(c, "当前地址适用的 Cookie · 保存在本机，重启后恢复").FontSize(11).TextColor(t.TextMuted)
				ui.Scroll(c).Grow(1).Children(func() {
					if len(cookies) == 0 {
						ui.Text(c, "还没有 Cookie，收到 Set-Cookie 后会自动保存。").TextColor(t.TextMuted)
					}
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						for _, cookie := range cookies {
							ui.Row(c).FillWidth().Padding(6, 10).Radius(t.Radius).Background(t.Surface).Children(func() {
								ui.Text(c, cookie.Name).Font("monospace").FontSize(12).Grow(1)
								ui.Text(c, "=").TextColor(t.TextMuted).FontSize(12)
								ui.Text(c, cookie.Value).Font("monospace").FontSize(12).TextColor(t.TextMuted).Grow(2).Selectable()
							})
						}
					})
				})
				ui.Button(c.Key("clear-cookies"), "清空所有 Cookie").OnClick(func() { a.engine.ClearCookies(); a.persist(); a.toast = "Cookie 已清空" })
			case 3:
				ui.Column(c).FillWidth().Gap(10).Padding(12).Radius(t.Radius).Background(t.Surface).Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "超时（秒）").TextColor(t.TextMuted)
						ui.TextInput(c.Key("timeout"), &a.timeout).Width(100).Label("超时秒数").OnChange(func() {
							n, err := strconv.Atoi(a.timeout)
							if err != nil || n < 1 || n > 3600 {
								a.notice = "超时为 1–3600 秒"
								return
							}
							r.TimeoutSeconds = n
							a.persist()
						})
					})
					ui.Checkbox(c.Key("redirect"), &r.FollowRedirects, "跟随重定向（最多 10 次）").OnChange(a.persist)
					ui.Text(c, "校验 TLS 证书 · 代理在「环境与全局请求头」中设置\n流连接的超时仅限制建立连接阶段，可随时停止。").TextColor(t.TextMuted).FontSize(12)
				})
			case 4:
				a.queryView(c)
			case 5:
				a.authView(c)
			case 6:
				a.extractionView(c)
			case 7:
				a.assertionsView(c)
			}
		})
	}, func() { a.responseView(c) }).Grow(1).MinHeight(120)
}
func (a *application) responseView(c *ui.Context) {
	t := c.Theme()
	services := c.Services()
	statusColor := t.TextMuted
	if code, ok := parseStatusPrefix(a.status); ok {
		switch {
		case code >= 200 && code < 300:
			statusColor = t.Success
		case code >= 300 && code < 400:
			statusColor = t.Accent
		case code >= 400 && code < 500:
			statusColor = t.Warning
		case code >= 500:
			statusColor = t.Danger
		}
	} else if a.status == "请求失败" || a.status == "已停止" {
		statusColor = t.Danger
	}
	ui.Column(c).Fill().Padding(10, 0, 0, 0).Gap(10).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "响应").Bold().FontSize(15)
			if a.status != "" && a.status != "准备就绪" {
				ui.Box(c).Padding(3, 10).Radius(999).Background(t.SurfaceHover).Children(func() {
					ui.Text(c, a.status).TextColor(statusColor).FontSize(11).Bold()
				})
			} else {
				ui.Text(c, a.status).TextColor(statusColor).FontSize(12)
			}
			ui.Spacer(c)
			ui.Button(c.Key("copy-response"), "复制响应").OnClick(func() { services.WriteClipboard(a.responseText()); a.toast = "响应已复制" })
		})
		ui.Tabs(c.Key("response-tabs"), &a.responseTab, "内容 / 消息", "响应头", "断言结果")
		if a.responseTab == 2 {
			a.assertionResultsView(c)
			return
		}
		text := a.responseText()
		if text == "" {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(10).Children(func() {
				ui.Text(c, "一次请求，从这里出发").FontSize(20).TextColor(t.TextMuted)
				ui.Text(c, "粘贴地址，或从左侧导入 curl。无需新建项目。").FontSize(12).TextColor(t.TextMuted)
			})
		} else {
			if a.responseVersion != text {
				a.responseEditor.Buffer.Reset(text)
				a.responseVersion = text
			}
			language := ""
			if a.responseTab == 0 && a.workspace.Draft.Protocol == "HTTP" {
				language = a.responseLanguage
			}
			// LanguageNamed returns nil for "", which is plain text.
			a.responseEditor.Buffer.SetLanguage(editor.LanguageNamed(language))
			editor.View(c.Key("response-code"), a.responseEditor).Label("响应内容").Grow(1).FillWidth().MinHeight(40)
		}
		if a.workspace.Draft.Protocol == "WebSocket" {
			ui.Row(c).Gap(10).Children(func() {
				ui.TextInput(c.Key("message"), &a.message).Grow(1).Label("WebSocket 消息").Placeholder("输入要发送的文本消息").OnSubmit(a.sendMessage)
				ui.PrimaryButton(c.Key("send-message"), "发送消息").Disabled(a.socket == nil).OnClick(a.sendMessage)
			})
		}
	})
}
func (a *application) settingsView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Children(func() {
		ui.Text(c, "环境与全局请求头").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("back"), "返回请求").OnClick(func() { a.page = 0 })
	})
	ui.Text(c, "所有配置只保存在本机。环境变量会用于 URL、Header 与正文。").TextColor(t.TextMuted)
	ui.Form(c, func() {
		ui.Field(c, "环境名称", func() {
			ui.TextInput(c.Key("env-name"), &a.envName).Width(220)
		}).Description("填入新名字即可新增环境")
		ui.Field(c, "环境变量", func() {
			ui.TextArea(c.Key("vars"), &a.variables).Grow(1).FillWidth().Font("monospace")
		}).Grow(1).Description("变量以 JSON 对象表示，值均为字符串，如 {\"host\":\"https://…\"}")
		ui.Field(c, "全局请求头", func() {
			ui.TextArea(c.Key("global-headers"), &a.globalHeaders).Grow(1).FillWidth().Font("monospace").Placeholder("Accept: application/json\nAuthorization: Bearer {{token}}")
		}).Grow(1)
		ui.Field(c, "CA 证书", func() {
			ui.Column(c).FillWidth().Gap(8).Padding(10).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.TextArea(c.Key("ca-cert"), &a.caCertPEM).FillWidth().Height(90).Font("monospace").Label("CA 证书内容").Placeholder("-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----")
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c.Key("import-ca"), "导入证书文件").Disabled(a.importingCA).OnClick(a.chooseCACertificates)
					ui.Button(c.Key("clear-ca"), "清除证书").Disabled(a.caCertPEM == "").OnClick(func() { a.caCertPEM = "" })
				})
			})
		}).Description("追加到系统信任库，用于 HTTPS、SSE 和 WSS。留空使用系统证书，保存配置后生效。")
		ui.Field(c, "代理", func() {
			ui.Row(c).FillWidth().Gap(8).Padding(10).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.TextInput(c.Key("proxy-url"), &a.proxyURL).Grow(1).Label("代理地址").Placeholder("http://127.0.0.1:8888 或 socks5://…")
				ui.TextInput(c.Key("proxy-bypass"), &a.proxyBypass).Grow(1).Label("不使用代理的地址").Placeholder("internal.example.com, .corp")
			})
		}).Description("用于 HTTP、SSE 和 WebSocket。留空直连；本机地址始终直连。")
	}).Grow(1).MinHeight(0)
	ui.Row(c).Gap(12).Children(func() {
		ui.PrimaryButton(c.Key("apply"), "保存配置").OnClick(a.applySettings)
		ui.Text(c, "普通环境值以明文保存；凭证请放入「机密变量」。").TextColor(t.TextMuted).FontSize(11)
	})
}
func (a *application) importView(c *ui.Context) {
	t := c.Theme()
	services := c.Services()
	ui.Row(c).Children(func() {
		ui.Text(c, "把 curl 放进来").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("back-import"), "返回请求").OnClick(func() { a.page = 0 })
	})
	ui.Text(c, "粘贴命令，变成可编辑的请求。只解析文本，导入时不会发送请求。").TextColor(t.TextMuted)
	ui.TextArea(c.Key("curl-text"), &a.curlInput).Label("curl 命令").Grow(1).FillWidth().Font("monospace").Placeholder("curl 'https://example.com/api' \\\n  -H 'Accept: application/json'")
	ui.Row(c).Gap(12).Children(func() {
		ui.PrimaryButton(c.Key("do-import"), "导入请求").OnClick(a.importCURL)
		ui.Button(c.Key("paste-curl"), "从剪贴板粘贴").OnClick(func() { a.curlInput = services.ReadClipboard() })
		ui.Text(c, "不执行脚本，不读取 @file").FontSize(11).TextColor(t.TextMuted)
	})
	ui.Divider(c)
	ui.Column(c).FillWidth().Gap(10).Padding(12).Radius(t.Radius).Background(t.Surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "导入集合").Bold().FontSize(14)
			ui.Text(c, "先预览再加入收藏").TextColor(t.TextMuted).FontSize(11)
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c.Key("import-curlgo"), "CurlGo 集合").Disabled(a.importingCollection).OnClick(func() { a.chooseCollection(collectionCurlGo) })
			ui.Button(c.Key("import-postman"), "Postman 集合").Disabled(a.importingCollection).OnClick(func() { a.chooseCollection(collectionPostman) })
			ui.Button(c.Key("import-bruno"), "Bruno 集合").Disabled(a.importingCollection).OnClick(func() { a.chooseCollection(collectionBruno) })
		})
	})
}
