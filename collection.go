package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type collectionKind int

const (
	collectionPostman collectionKind = iota
	collectionBruno
)

const (
	maxCollectionFile = 16 << 20
	maxSavedRequests  = 5000
)

// readCollection parses a picked file or folder; it never sends requests.
func readCollection(kind collectionKind, path string) (core.CollectionImport, error) {
	if kind == collectionBruno {
		return core.ImportBrunoDirectory(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return core.CollectionImport{}, fmt.Errorf("无法读取集合文件：%w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCollectionFile+1))
	if err != nil {
		return core.CollectionImport{}, fmt.Errorf("无法读取集合文件：%w", err)
	}
	if len(data) > maxCollectionFile {
		return core.CollectionImport{}, fmt.Errorf("集合文件超过 16 MiB")
	}
	return core.ImportPostman(data)
}

func (a *application) chooseCollection(kind collectionKind) {
	if a.importingCollection || a.closing {
		return
	}
	a.importingCollection = true
	opts := mygo.OpenDialogOptions{
		Parent: a.win, Title: "导入 Postman 集合", ButtonLabel: "导入",
		Message: "选择从 Postman 导出的 Collection v2.1 JSON 文件。",
		Filters: []mygo.FileFilter{{Name: "Postman 集合", Extensions: []string{"json"}}},
	}
	if kind == collectionBruno {
		opts = mygo.OpenDialogOptions{
			Parent: a.win, Title: "导入 Bruno 集合", ButtonLabel: "导入", Directory: true,
			Message: "选择包含 bruno.json 的集合文件夹。",
		}
	}
	picker := a.pickCollectionFile
	go func() {
		paths, err := picker(opts)
		var imported core.CollectionImport
		if err == nil && len(paths) > 0 {
			imported, err = readCollection(kind, paths[0])
		}
		a.post(func() {
			a.importingCollection = false
			if a.closed || a.closing {
				return
			}
			if err != nil {
				a.notice = err.Error()
				return
			}
			if len(paths) == 0 {
				return
			}
			a.notice = ""
			a.collectionImport = &imported
			a.page = 3
		})
	}()
}

func (a *application) cancelCollectionImport() {
	a.collectionImport = nil
	a.page = 2
}

// uniqueEnvironment keeps existing environments untouched when names collide.
func uniqueEnvironment(envs map[string]map[string]string, name string) string {
	if _, taken := envs[name]; !taken {
		return name
	}
	for i := 2; ; i++ {
		candidate := name + " " + strconv.Itoa(i)
		if _, taken := envs[candidate]; !taken {
			return candidate
		}
	}
}

// confirmCollectionImport saves the previewed requests as new favorites and
// adds each imported environment under a name that does not collide.
func (a *application) confirmCollectionImport() {
	c := a.collectionImport
	if c == nil || len(c.Requests) == 0 {
		return
	}
	if len(a.workspace.Requests)+len(c.Requests) > maxSavedRequests {
		a.notice = fmt.Sprintf("收藏最多 %d 个请求，请先移除部分收藏再导入", maxSavedRequests)
		return
	}
	for _, r := range c.Requests {
		r.ID = core.NewID()
		r.Headers = append([]core.Pair(nil), r.Headers...)
		r.FormParts = append([]core.FormPart(nil), r.FormParts...)
		a.workspace.Requests = append(a.workspace.Requests, r)
	}
	if a.workspace.Settings.Environments == nil {
		a.workspace.Settings.Environments = map[string]map[string]string{}
	}
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	selected := ""
	for _, name := range names {
		vars := make(map[string]string, len(c.Environments[name]))
		for k, v := range c.Environments[name] {
			vars[k] = v
		}
		saved := uniqueEnvironment(a.workspace.Settings.Environments, name)
		a.workspace.Settings.Environments[saved] = vars
		if selected == "" || name == c.Name {
			selected = saved
		}
	}
	if selected != "" {
		a.workspace.Settings.Environment = selected
	}
	a.toast = fmt.Sprintf("已导入 %d 个请求到收藏", len(c.Requests))
	a.collectionImport = nil
	a.libraryTab = 0
	a.page = 0
	a.persist()
}

func (a *application) collectionView(c *ui.Context) {
	t := c.Theme()
	imported := a.collectionImport
	ui.Row(c).Children(func() {
		ui.Text(c, "导入「"+short(imported.Name, 40)+"」").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("cancel-collection"), "取消导入").OnClick(a.cancelCollectionImport)
	})
	summary := fmt.Sprintf("将加入 %d 个请求到收藏", len(imported.Requests))
	if len(imported.Environments) > 0 {
		summary += fmt.Sprintf("，并新增 %d 个环境", len(imported.Environments))
	}
	if len(imported.Requests) == 0 {
		summary = "没有可导入的请求"
	}
	ui.Text(c, summary+"。确认前不会修改工作区，也不会发送请求。").TextColor(t.TextMuted)
	ui.Scroll(c.Key("collection-preview")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			if len(imported.Warnings) > 0 {
				ui.Text(c, "需要注意").Bold()
				for _, w := range imported.Warnings {
					ui.Text(c, "· "+w).FontSize(12).Selectable()
				}
				ui.Divider(c)
			}
			for _, r := range imported.Requests {
				ui.Text(c, r.Method+"  "+short(r.Name, 60)).FontSize(12)
				ui.Text(c, short(r.URL, 90)).FontSize(11).TextColor(t.TextMuted)
			}
		})
	})
	ui.Row(c).Gap(12).Children(func() {
		ui.PrimaryButton(c.Key("confirm-collection"), "加入收藏").Disabled(len(imported.Requests) == 0).OnClick(a.confirmCollectionImport)
		ui.Text(c, "不执行脚本 · 文件在发送时才读取").FontSize(11).TextColor(t.TextMuted)
	})
}
