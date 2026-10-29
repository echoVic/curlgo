package main

import (
	"fmt"
	"strings"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

const ungroupedFilter = "-ungrouped-"

type groupOption struct{ id, label string }

func (a *application) groupExists(id string) bool {
	for _, g := range a.workspace.Groups {
		if g.ID == id {
			return true
		}
	}
	return false
}

func (a *application) groupOptions(first string) []groupOption {
	opts := []groupOption{{label: first}}
	paths := core.GroupPaths(a.workspace.Groups)
	for _, g := range a.workspace.Groups {
		opts = append(opts, groupOption{g.ID, paths[g.ID]})
	}
	return opts
}

// IDs remain the select values even when imported groups have identical names.
func groupSelect(c *ui.Context, selected *string, options []groupOption, label string, change func()) ui.Element {
	t := c.Theme()
	current := options[0].label
	for _, o := range options {
		if o.id == *selected {
			current = o.label
		}
	}
	sel := ui.SelectBase(c, selected)
	sel.Trigger.Height(32).Padding(6, 10).Radius(t.Radius).Background(t.Surface).
		BorderWidth(1).BorderColor(t.Border).Label(label).OnChange(change).
		Children(func() {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, short(current, 42)).SingleLine().Grow(1)
				ui.Text(c, "⌄").TextColor(t.TextMuted)
			})
		})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Radius).Background(t.Surface).BorderWidth(1).BorderColor(t.Border)
		ui.Scroll(c).MaxHeight(280).Children(func() {
			for _, option := range options {
				item := sel.Item(option.id).Padding(7, 10).Radius(t.Radius).Label("选择分组 " + option.label)
				if item.Highlighted() {
					item.Background(t.Accent).TextColor(t.AccentText)
				}
				item.Children(func() { ui.Text(c, short(option.label, 60)).SingleLine() })
			}
		})
	})
	return sel.Trigger
}

func (a *application) openGroups() {
	a.editGroupID, a.groupName, a.groupParent = "", "", ""
	a.page, a.notice = 4, ""
}

func (a *application) editGroup(id string) {
	for _, g := range a.workspace.Groups {
		if g.ID == id {
			a.editGroupID, a.groupName, a.groupParent = g.ID, g.Name, g.ParentID
			a.notice = ""
			return
		}
	}
}

func (a *application) saveGroup() {
	if a.closing || a.closed {
		return
	}
	name := strings.TrimSpace(a.groupName)
	for _, g := range a.workspace.Groups {
		if g.ID != a.editGroupID && g.ParentID == a.groupParent && g.Name == name {
			a.notice = "上级分组中已有同名分组"
			return
		}
	}
	groups := append([]core.RequestGroup(nil), a.workspace.Groups...)
	id := a.editGroupID
	if id == "" {
		id = core.NewID()
		groups = append(groups, core.RequestGroup{ID: id, Name: name, ParentID: a.groupParent})
	} else {
		for i := range groups {
			if groups[i].ID == id {
				groups[i].Name, groups[i].ParentID = name, a.groupParent
			}
		}
	}
	if err := core.ValidateRequestGroups(groups); err != nil {
		a.notice = err.Error()
		return
	}
	a.workspace.Groups = groups
	a.editGroupID, a.groupName, a.notice = id, name, ""
	a.toast = "分组已保存"
	a.persist()
}

func (a *application) deleteGroup() {
	if a.closing || a.closed || a.editGroupID == "" {
		return
	}
	ids := core.GroupSubtree(a.workspace.Groups, a.editGroupID)
	kept := a.workspace.Groups[:0]
	for _, g := range a.workspace.Groups {
		if !ids[g.ID] {
			kept = append(kept, g)
		}
	}
	a.workspace.Groups = kept
	for i := range a.workspace.Requests {
		if ids[a.workspace.Requests[i].GroupID] {
			a.workspace.Requests[i].GroupID = ""
		}
	}
	if ids[a.workspace.Draft.GroupID] {
		a.workspace.Draft.GroupID = ""
	}
	if ids[a.groupFilter] {
		a.groupFilter = ungroupedFilter
	}
	a.editGroupID, a.groupName, a.groupParent = "", "", ""
	a.toast = "分组已删除，请求已移到未分组"
	a.persist()
}

func (a *application) moveDraftGroup() {
	if a.busy || a.closing || a.closed {
		return
	}
	for i := range a.workspace.Requests {
		if a.workspace.Requests[i].ID == a.workspace.Draft.ID {
			a.workspace.Requests[i].GroupID = a.workspace.Draft.GroupID
		}
	}
	a.persist()
}

func (a *application) savedDraft() bool {
	for _, r := range a.workspace.Requests {
		if r.ID == a.workspace.Draft.ID {
			return true
		}
	}
	return false
}

func (a *application) copySavedRequest(id string) {
	if a.busy || a.closing || a.closed {
		return
	}
	if len(a.workspace.Requests) >= maxSavedRequests {
		a.notice = "收藏最多 5000 个请求"
		return
	}
	for _, r := range a.workspace.Requests {
		if r.ID != id {
			continue
		}
		r.ID, r.Name = core.NewID(), r.Name+" 副本"
		r.Headers = append([]core.Pair(nil), r.Headers...)
		r.FormParts = append([]core.FormPart(nil), r.FormParts...)
		r.QueryParams = append([]core.QueryParam(nil), r.QueryParams...)
		a.workspace.Requests = append(a.workspace.Requests, r)
		a.selectRequest(r)
		a.toast = "收藏已复制"
		return
	}
}

func (a *application) groupsView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Children(func() {
		ui.Text(c, "管理收藏分组").FontSize(23).Bold()
		ui.Spacer(c)
		ui.Button(c.Key("back-groups"), "返回请求").OnClick(func() { a.page = 0 })
	})
	ui.Text(c, "用分组整理收藏；删除分组会将其中请求移到未分组。").TextColor(t.TextMuted)
	type row struct {
		group core.RequestGroup
		depth int
	}
	var rows []row
	children := map[string][]core.RequestGroup{}
	for _, g := range a.workspace.Groups {
		children[g.ParentID] = append(children[g.ParentID], g)
	}
	var walk func(string, int)
	walk = func(parent string, depth int) {
		if depth >= core.MaxGroupDepth {
			return
		}
		for _, g := range children[parent] {
			rows = append(rows, row{g, depth})
			walk(g.ID, depth+1)
		}
	}
	walk("", 0)
	a.groupList.Key = func(i int) any { return rows[i].group.ID }
	ui.List(c.Key("groups-list"), &a.groupList, len(rows), func(i int) {
		r := rows[i]
		ui.Row(c).Padding(0, 0, 0, float32(r.depth*16)).Children(func() {
			ui.Button(c.Key("edit"), "▸ "+r.group.Name).FillWidth().
				Label("编辑分组 " + core.GroupPath(a.workspace.Groups, r.group.ID)).
				OnClick(func() { a.editGroup(r.group.ID) })
		})
	}).Grow(1).MinHeight(60).Children(func() {
		if len(rows) == 0 {
			ui.Text(c, "创建第一个分组，把常用接口放在一起。").TextColor(t.TextMuted)
		}
	})
	ui.Row(c).Gap(10).Children(func() {
		ui.Button(c.Key("new-group"), "新建分组").OnClick(func() { a.editGroupID, a.groupName, a.groupParent = "", "", "" })
		ui.Button(c.Key("new-child"), "新建子分组").Disabled(a.editGroupID == "").OnClick(func() {
			a.groupParent = a.editGroupID
			a.editGroupID, a.groupName = "", ""
		})
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf("%d 个分组", len(rows))).FontSize(11).TextColor(t.TextMuted)
	})
	ui.Divider(c)
	ui.TextInput(c.Key("group-name"), &a.groupName).FillWidth().Label("分组名称").Placeholder("分组名称")
	options := a.groupOptions("顶层分组")
	descendants := core.GroupSubtree(a.workspace.Groups, a.editGroupID)
	filtered := options[:0]
	for _, o := range options {
		if !descendants[o.id] {
			filtered = append(filtered, o)
		}
	}
	ui.Row(c).Gap(12).Children(func() {
		ui.Text(c, "位置").TextColor(t.TextMuted)
		groupSelect(c.Key("group-parent"), &a.groupParent, filtered, "上级分组", nil).Grow(1)
	})
	ui.Row(c).Gap(12).Children(func() {
		label := "创建分组"
		if a.editGroupID != "" {
			label = "保存分组"
		}
		ui.PrimaryButton(c.Key("save-group"), label).OnClick(a.saveGroup)
		ui.Button(c.Key("delete-group"), "删除分组，保留请求").Disabled(a.editGroupID == "").OnClick(a.deleteGroup)
	})
}
