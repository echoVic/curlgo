package main

import (
	"encoding/json"
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
	collectionCurlGo
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
	if kind == collectionCurlGo {
		return core.ImportCurlGoCollection(data)
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
	} else if kind == collectionCurlGo {
		opts.Title = "导入 CurlGo 集合"
		opts.Message = "选择 CurlGo 导出的集合 JSON 文件。"
		opts.Filters = []mygo.FileFilter{{Name: "CurlGo 集合", Extensions: []string{"json"}}}
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
	if c == nil || len(c.Requests) == 0 && len(c.Groups) == 0 {
		return
	}
	if len(a.workspace.Requests)+len(c.Requests) > maxSavedRequests {
		a.notice = fmt.Sprintf("收藏最多 %d 个请求，请先移除部分收藏再导入", maxSavedRequests)
		return
	}
	if err := core.ValidateRequestGroups(c.Groups); err != nil {
		a.notice = err.Error()
		return
	}
	if len(a.workspace.Groups)+len(c.Groups) > core.MaxGroups {
		a.notice = "分组最多 5000 个"
		return
	}
	groupIDs := make(map[string]string, len(c.Groups))
	for _, g := range c.Groups {
		groupIDs[g.ID] = core.NewID()
	}
	for _, r := range c.Requests {
		if r.GroupID != "" && groupIDs[r.GroupID] == "" {
			a.notice = "请求所属分组不存在"
			return
		}
	}
	next := cloneWorkspace(a.workspace)
	for _, g := range c.Groups {
		g.ID, g.ParentID = groupIDs[g.ID], groupIDs[g.ParentID]
		next.Groups = append(next.Groups, g)
	}
	for _, r := range c.Requests {
		r.ID = core.NewID()
		r.GroupID = groupIDs[r.GroupID]
		r.Headers = append([]core.Pair(nil), r.Headers...)
		r.FormParts = append([]core.FormPart(nil), r.FormParts...)
		r.QueryParams = append([]core.QueryParam(nil), r.QueryParams...)
		next.Requests = append(next.Requests, r)
	}
	if next.Settings.Environments == nil {
		next.Settings.Environments = map[string]map[string]string{}
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
		saved := uniqueEnvironment(next.Settings.Environments, name)
		next.Settings.Environments[saved] = vars
		if selected == "" || name == c.Name && c.Environment == "" || name == c.Environment {
			selected = saved
		}
	}
	if selected != "" {
		next.Settings.Environment = selected
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil || len(data) > maxCollectionFile {
		a.notice = "导入后工作区超过 16 MiB，请先清理收藏或历史"
		return
	}
	a.workspace = next
	a.syncSettings()
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
	if len(imported.Groups) > 0 {
		summary += fmt.Sprintf("，保留 %d 个分组", len(imported.Groups))
	}
	if len(imported.Requests) == 0 && len(imported.Groups) == 0 {
		summary = "没有可导入的请求"
	}
	ui.Text(c, summary+"。确认前不会修改工作区，也不会发送请求。").TextColor(t.TextMuted)
	if len(imported.Warnings) > 0 {
		ui.Scroll(c.Key("collection-warnings")).FillWidth().MaxHeight(140).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Padding(10).Radius(t.Radius).Background(t.Surface).Children(func() {
				ui.Text(c, "需要注意").Bold().TextColor(t.Warning)
				for _, w := range imported.Warnings {
					ui.Text(c, "· "+w).FontSize(12).Selectable()
				}
			})
		})
	}
	paths := core.GroupPaths(imported.Groups)
	a.collectionList.Key = func(i int) any {
		if i >= 0 && i < len(imported.Requests) {
			return imported.Requests[i].ID
		}
		return i
	}
	ui.List(c.Key("collection-preview"), &a.collectionList, len(imported.Requests), func(i int) {
		r := imported.Requests[i]
		mc := methodColor(r.Method)
		ui.Column(c).FillWidth().Gap(4).Padding(8).Radius(t.Radius).Background(t.Surface).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Padding(1, 6).Radius(4).Background(mc).Children(func() {
					ui.Text(c, r.Method).FontSize(10).Bold().TextColor(ui.Hex("#ffffff"))
				})
				ui.Text(c, short(r.Name, 50)).FontSize(12).Grow(1)
			})
			if r.GroupID != "" {
				ui.Text(c, paths[r.GroupID]).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, short(r.URL, 90)).FontSize(11).TextColor(t.TextMuted)
		})
	}).Grow(1).MinHeight(0)
	ui.Row(c).Gap(12).Children(func() {
		ui.PrimaryButton(c.Key("confirm-collection"), "加入收藏").Disabled(len(imported.Requests) == 0 && len(imported.Groups) == 0).OnClick(a.confirmCollectionImport)
		ui.Text(c, "不执行脚本 · 文件在发送时才读取").FontSize(11).TextColor(t.TextMuted)
	})
}
