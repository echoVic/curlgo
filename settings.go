package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *application) appSettingsView(c *ui.Context) {
	t := c.Theme()
	prefs := a.updatePrefs
	if a.appUpdater == nil {
		prefs.automaticChecks = true
	}
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "应用设置").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("settings-back"), "返回请求").OnClick(func() { a.page = 0 })
	})
	ui.Text(c, "调整外观，管理 CurlGo 的软件更新。").TextColor(t.TextMuted)
	ui.Scroll(c.Key("app-settings-scroll")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(18).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Padding(18).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.Text(c, "外观").FontSize(16).Bold()
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "主题").Grow(1)
					ui.Select(c.Key("app-theme"), &a.workspace.Settings.Theme, []string{"浅色", "深色"}).Width(120).Label("应用主题").OnChange(a.persist)
				})
			})
			ui.Column(c).FillWidth().Gap(14).Padding(18).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.Text(c, "版本与更新").FontSize(16).Bold()
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, "当前版本").TextColor(t.TextMuted)
						ui.Text(c, applicationVersion()).FontSize(18).Bold()
					})
					ui.Button(c.Key("check-for-updates"), "检查更新").Disabled(a.appUpdater == nil).OnClick(a.checkForUpdates)
				})
				if !prefs.enabled {
					message := "此安装无法自动更新：未配置更新，或安装目录没有写入权限。请使用正式安装包；包管理器安装的应用请由包管理器更新。"
					if mygo.IsDev() {
						message = "开发构建不支持更新。正式安装后，还需安装目录有写入权限才能下载并安装更新。"
					}
					ui.Text(c, message).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "检查更新会联网。下载并安装前会验证发布签名，更新在下次启动时生效。").TextColor(t.TextMuted)
				}
				last := "尚未成功检查"
				if !prefs.lastCheck.IsZero() {
					last = prefs.lastCheck.Local().Format("2006-01-02 15:04")
				}
				ui.Text(c, "上次成功检查："+last).TextColor(t.TextMuted).FontSize(12)
				ui.Divider(c)
				ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, "后台自动检查").Bold()
						ui.Text(c, "默认开启，每 4 小时检查一次；发现新版本时提醒。").TextColor(t.TextMuted).FontSize(12)
					})
					ui.Switch(c.Key("automatic-checks"), &prefs.automaticChecks).Label("允许后台自动检查").Disabled(!prefs.enabled).OnChange(func() { a.setAutomaticChecks(prefs.automaticChecks) })
				})
				ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, "自动下载并安装").Bold()
						ui.Text(c, "默认关闭。仅在你开启后，后台安装更新，下次启动生效。").TextColor(t.TextMuted).FontSize(12)
					})
					ui.Switch(c.Key("automatic-downloads"), &prefs.automaticDownloads).Label("允许自动下载并安装").Disabled(!prefs.enabled || !prefs.automaticChecks).OnChange(func() { a.setAutomaticDownloads(prefs.automaticDownloads) })
				})
				if !prefs.automaticChecks {
					ui.Text(c, "后台检查已关闭，自动下载暂停。仍可手动检查更新。").TextColor(t.TextMuted).FontSize(12)
				}
			})
			ui.Column(c).FillWidth().Gap(12).Padding(18).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.Text(c, "问题反馈").FontSize(16).Bold()
				ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "遇到问题或有建议？欢迎在 GitHub 上告诉我们。").TextColor(t.TextMuted).Grow(1)
					ui.Link(c, "报告问题或建议", feedbackURL)
				})
			})
			a.shortcutSettingsCard(c)
		})
	})
}

// shortcutSettingsCard lists every customizable action. Clicking 更改
// focuses a field that records the next chord; Escape cancels it and
// Backspace restores the action's default.
func (a *application) shortcutSettingsCard(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(12).Padding(18).Radius(t.Radius).Background(t.Surface).Children(func() {
		ui.Text(c, "快捷键").FontSize(16).Bold()
		ui.Text(c, "点击「更改」后按下新组合；Esc 取消，Backspace 恢复默认。").TextColor(t.TextMuted).FontSize(12)
		for _, shortcut := range a.shortcuts() {
			shortcut := shortcut
			recording := a.recordingShortcut == shortcut.Action
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, shortcut.Label).Grow(1)
				if recording {
					ui.TextInput(c.Key("shortcut-recording"), &a.shortcutCapture).Width(220).ReadOnly(true).Placeholder("按下新的快捷键…").Label("新的快捷键").Focus().OnTextKey(a.captureShortcut)
				} else {
					ui.Text(c, formatChord(shortcut.Mods, shortcut.Key, shortcutPlatform())).TextColor(t.TextMuted)
					ui.Button(c.Key("shortcut-change-"+shortcut.Action), "更改").Label("更改 " + shortcut.Label).OnClick(func() {
						a.startShortcutRecording(shortcut.Action)
					})
				}
				ui.Button(c.Key("shortcut-reset-"+shortcut.Action), "恢复默认").Label("恢复默认 " + shortcut.Label).Disabled(a.shortcutIsDefault(shortcut.Action)).OnClick(func() {
					a.resetShortcut(shortcut.Action)
				})
			})
		}
		if a.shortcutMessage != "" {
			ui.Text(c, a.shortcutMessage).TextColor(t.Danger).FontSize(12)
		}
		ui.Row(c).Children(func() {
			ui.Button(c.Key("shortcuts-reset-all"), "全部恢复默认").OnClick(a.resetAllShortcuts)
		})
	})
}
