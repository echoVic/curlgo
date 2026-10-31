package main

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/echoVic/curlgo/internal/keychain"
	"github.com/echoVic/curlgo/internal/vault"
	"github.com/egoist/mygo/ui"
)

func (a *application) attachSecrets() {
	if a.readOnly || a.store == nil {
		a.secretNotice = "当前临时会话无法保存机密变量"
		return
	}
	if runtime.GOOS != "darwin" {
		a.secretNotice = "桌面机密存储目前支持 macOS；CLI 可使用 --secrets-file"
		return
	}
	v, err := vault.Open(filepath.Join(a.store.Dir, "secret-vault.json"), keychain.New())
	if err != nil {
		a.secretNotice = "机密文件不可读，原文件已保留；普通请求仍可使用"
		return
	}
	a.secrets = v
}

func (a *application) secretValues(environment string) map[string]string {
	if a.secrets == nil {
		return nil
	}
	return a.secrets.Values(environment)
}

func (a *application) clearSecretEditor() {
	a.secretEditorGeneration++
	a.secretName, a.secretValue = "", ""
	a.showSecret = false
}

func (a *application) cancelSecretUnlock() {
	a.secretGeneration++
	if a.secretCancel != nil {
		a.secretCancel()
		a.secretCancel = nil
		if a.secrets != nil {
			a.secrets.Lock()
		}
	}
	a.secretBusy = false
}

func (a *application) leaveSecretPage() {
	a.cancelSecretUnlock()
	a.clearSecretEditor()
	a.secretPageActive = false
}

func (a *application) lockSecrets() {
	a.leaveSecretPage()
	if a.secrets != nil {
		a.secrets.Lock()
	}
}

func (a *application) unlockSecrets() {
	if a.secrets == nil || a.secretBusy || a.closed || a.closing || a.readOnly {
		return
	}
	a.secretBusy = true
	a.secretGeneration++
	generation := a.secretGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.secretCancel = cancel
	v := a.secrets
	go func() {
		err := v.Unlock(ctx)
		a.post(func() {
			if a.closed || a.closing || a.secretGeneration != generation {
				return
			}
			cancel()
			a.secretCancel = nil
			a.secretBusy = false
			if err != nil {
				a.secretNotice = "解锁失败：请检查登录钥匙串与机密文件；原数据已保留"
				return
			}
			a.secretNotice = ""
			a.toast = "机密变量已解锁"
		})
	}()
}

func (a *application) saveSecret() {
	if a.secrets == nil || a.secretBusy || a.readOnly || a.closed || a.closing {
		return
	}
	if err := a.secrets.Set(a.workspace.Settings.Environment, a.secretName, a.secretValue); err != nil {
		a.secretNotice = "保存失败：请检查变量名、值的大小、解锁状态和文件权限"
		return
	}
	a.clearSecretEditor()
	a.secretNotice = ""
	a.toast = "机密变量已加密保存"
}

func (a *application) deleteSecret(name string) {
	if a.secrets == nil || a.readOnly || a.closed || a.closing {
		return
	}
	if err := a.secrets.Delete(a.workspace.Settings.Environment, name); err != nil {
		a.secretNotice = "删除失败：机密变量未更改"
		return
	}
	a.clearSecretEditor()
	a.secretNotice = ""
	a.toast = "机密变量已删除"
}

func (a *application) secretsView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, "机密变量").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("back-secrets"), "返回请求").OnClick(func() { a.leaveSecretPage(); a.page = 0 })
	})
	ui.Text(c, "用 {{secret.token}} 引用 · 按环境隔离 · 值单独加密保存在本机").TextColor(t.TextMuted).FontSize(12)
	var environments []string
	for name := range a.workspace.Settings.Environments {
		environments = append(environments, name)
	}
	sort.Strings(environments)
	unlocked := a.secrets != nil && a.secrets.Unlocked()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "环境").TextColor(t.TextMuted)
		ui.Select(c.Key("secret-environment"), &a.workspace.Settings.Environment, environments).Width(180).
			Label("机密变量环境").Disabled(a.secretBusy).OnChange(func() { a.clearSecretEditor(); a.persist() })
		ui.Spacer(c)
		if unlocked {
			ui.Box(c).Padding(4, 10).Radius(999).Background(t.SurfaceHover).Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(999).Background(t.Success)
					ui.Text(c, "已解锁").TextColor(t.Success).FontSize(11)
				})
			})
			ui.Button(c.Key("lock-secrets"), "锁定").Label("锁定机密变量").OnClick(a.lockSecrets)
		} else {
			label := "解锁"
			if a.secretBusy {
				label = "正在解锁…"
			}
			ui.PrimaryButton(c.Key("unlock-secrets"), label).Disabled(a.secrets == nil || a.secretBusy || a.readOnly).OnClick(a.unlockSecrets)
		}
	})
	if a.secretNotice != "" {
		ui.Text(c, a.secretNotice).TextColor(t.Danger).FontSize(12).FillWidth()
	}
	if !unlocked {
		ui.Column(c).Grow(1).FillWidth().Center().Gap(10).Children(func() {
			ui.Text(c, "解锁后查看和编辑当前环境的机密变量").FontSize(16).TextColor(t.TextMuted)
			ui.Text(c, "名称也在加密文件中；锁定时含机密引用的请求不会发送。").FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	ui.Column(c).FillWidth().Gap(8).Padding(12).Radius(t.Radius).Background(t.Surface).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			editorID := strconv.FormatUint(a.secretEditorGeneration, 10)
			ui.TextInput(c.Key("secret-name-"+editorID), &a.secretName).Width(200).Label("机密变量名称").Placeholder("token（不含 secret.）")
			input := ui.TextInput(c.Key("secret-value-"+editorID), &a.secretValue).Grow(1).MinWidth(0).Label("机密变量值").Placeholder("仅用于当前编辑").
				OnPaste(func(text string) bool {
					if strings.ContainsAny(text, "\r\n") {
						a.secretNotice = "编辑框不接受换行粘贴，原值未更改"
						return true
					}
					return false
				})
			if !a.showSecret {
				input.Password()
			}
		})
		ui.Row(c).Gap(10).Children(func() {
			ui.Checkbox(c.Key("show-secret"), &a.showSecret, "显示编辑值")
			ui.Spacer(c)
			ui.Button(c.Key("cancel-secret"), "取消编辑").OnClick(a.clearSecretEditor)
			ui.PrimaryButton(c.Key("save-secret"), "保存机密变量").OnClick(a.saveSecret)
		})
	})
	ui.Divider(c)
	ui.Scroll(c.Key("secret-list")).Grow(1).MinHeight(0).Children(func() {
		values := a.secretValues(a.workspace.Settings.Environment)
		var names []string
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			if len(names) == 0 {
				ui.Text(c, "当前环境还没有机密变量。").TextColor(t.TextMuted)
			}
			for _, name := range names {
				ui.Row(c.Key(name)).FillWidth().Gap(10).Padding(8, 12).Radius(t.Radius).Background(t.Surface).Children(func() {
					ui.Text(c, "{{secret."+name+"}}").Grow(1).Font("monospace")
					ui.Text(c, "••••••••").TextColor(t.TextMuted).FontSize(11)
					ui.Button(c.Key("edit"), "编辑").Label("编辑机密 " + name).OnClick(func() {
						a.clearSecretEditor()
						a.secretName = name
						a.secretValue = a.secretValues(a.workspace.Settings.Environment)[name]
						a.showSecret = false
					})
					ui.Button(c.Key("delete"), "删除").Label("删除机密 " + name).OnClick(func() { a.deleteSecret(name) })
				})
			}
		})
	})
	ui.Text(c, "保存覆盖同名值。请求与集合只保存引用；响应仍可能包含服务器返回的凭证。").FontSize(11).TextColor(t.TextMuted)
}
