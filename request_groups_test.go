package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func TestGroupsSnapshot(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Groups = []core.RequestGroup{{ID: "group", Name: "API"}}
	snapshot := cloneWorkspace(a.workspace)
	a.workspace.Groups[0].Name = "Changed"
	if snapshot.Groups[0].Name != "API" {
		t.Fatal("后台保存快照共享分组切片")
	}
}

func TestGroupedFavoritesAtCapacity(t *testing.T) {
	a, _ := testApp(t)
	for i := range maxSavedRequests {
		id := fmt.Sprintf("group-%d", i)
		a.workspace.Groups = append(a.workspace.Groups, core.RequestGroup{ID: id, Name: fmt.Sprintf("Group %d", i)})
		r := appDraft("https://example.test")
		r.ID, r.Name, r.GroupID = fmt.Sprintf("request-%d", i), fmt.Sprintf("Request %d", i), id
		a.workspace.Requests = append(a.workspace.Requests, r)
	}
	tt := ui.NewTester(a.view, 940, 680)
	a.search = "Group 4999"
	tt.Frame()
	if !tt.HasText("Request 4999") || tt.HasText("Request 0") {
		t.Fatal("收藏上限时的分组路径搜索没有正确筛选虚拟列表")
	}
	a.search = ""
	a.groupFilter = "group-4998"
	tt.Frame()
	if !tt.HasText("Request 4998") || tt.HasText("Request 4999") {
		t.Fatal("收藏上限时的分组筛选错误")
	}
	a.collectionImport = &core.CollectionImport{Name: "Large", Requests: a.workspace.Requests, Groups: a.workspace.Groups}
	a.page = 3
	tt.Frame()
	if !tt.HasText("Request 0") || tt.HasText("Request 4999") {
		t.Fatal("集合预览没有虚拟化请求列表")
	}
	if r, ok := tt.Find("加入收藏"); !ok || r.Y+r.H > 680 {
		t.Fatal("大集合预览隐藏了导入操作")
	}
}

func TestRequestGroupsView(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "管理分组")
	inputUI(t, tt, "分组名称", "API")
	clickUI(t, tt, "创建分组")
	if len(a.workspace.Groups) != 1 {
		t.Fatal("未创建顶层分组")
	}
	root := a.workspace.Groups[0].ID
	clickUI(t, tt, "新建子分组")
	inputUI(t, tt, "分组名称", "Users")
	clickUI(t, tt, "创建分组")
	child := a.workspace.Groups[1].ID
	if a.workspace.Groups[1].ParentID != root {
		t.Fatal("子分组没有关联父 ID")
	}
	inputUI(t, tt, "分组名称", "People")
	clickUI(t, tt, "保存分组")
	if core.GroupPath(a.workspace.Groups, child) != "API / People" {
		t.Fatal("改名没有保存")
	}
	clickUI(t, tt, "编辑分组 API")
	clickUI(t, tt, "新建子分组")
	inputUI(t, tt, "分组名称", "People")
	clickUI(t, tt, "创建分组")
	if len(a.workspace.Groups) != 2 || !strings.Contains(a.notice, "同名") {
		t.Fatal("同级重名未拒绝")
	}
	for _, label := range []string{"分组名称", "上级分组", "创建分组", "返回请求"} {
		r, ok := tt.Find(label)
		if !ok || r.W < 40 || r.X+r.W > 940 || r.Y+r.H > 680 {
			t.Fatalf("最小窗口分组管理不可用：%s %+v", label, r)
		}
	}
	clickUI(t, tt, "知道了")
	clickUI(t, tt, "编辑分组 API")
	clickUI(t, tt, "删除分组，保留请求")
	if len(a.workspace.Groups) != 0 {
		t.Fatal("界面删除分组没有应用")
	}
}

func TestGroupedFavorites(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Groups = []core.RequestGroup{{ID: "root", Name: "API"}, {ID: "child", Name: "People", ParentID: "root"}}
	r := appDraft("https://example.test/?a=1")
	r.Name, r.GroupID = "Grouped", "child"
	r.Headers = []core.Pair{{Key: "X-Test", Value: "saved"}}
	r.FormParts = []core.FormPart{{ID: "part", Kind: core.FormText, Name: "name", Value: "saved"}}
	r.QueryParams = []core.QueryParam{{ID: "query", Key: "a", Value: "saved", Disabled: true}}
	a.workspace.Requests = []core.Request{r, {ID: "loose", Name: "Loose"}, {ID: "unknown", Name: "Unknown", GroupID: "missing"}}
	a.selectRequest(r)
	a.workspace.Draft.URL = "https://example.test/unsaved"
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "所属分组")
	clickUI(t, tt, "选择分组 API")
	if a.workspace.Requests[0].GroupID != "root" || a.workspace.Requests[0].URL != r.URL || a.workspace.Draft.URL != "https://example.test/unsaved" {
		t.Fatal("移动分组覆盖了草稿或收藏内容")
	}
	a.groupFilter = "root"
	tt.Frame()
	if !tt.HasText("Grouped") || tt.HasText("Loose") {
		t.Fatal("父分组筛选错误")
	}
	a.groupFilter = ungroupedFilter
	tt.Frame()
	if !tt.HasText("Loose") || !tt.HasText("Unknown") || tt.HasText("Grouped") {
		t.Fatal("未分组筛选未兼容丢失的组 ID")
	}
	a.copySavedRequest(r.ID)
	if len(a.workspace.Requests) != 4 || a.workspace.Requests[3].ID == r.ID || a.workspace.Requests[3].GroupID != "root" {
		t.Fatal("复制未生成独立收藏")
	}
	cp := &a.workspace.Requests[3]
	cp.Headers[0].Value, cp.FormParts[0].Value, cp.QueryParams[0].Value = "copy", "copy", "copy"
	if a.workspace.Requests[0].Headers[0].Value != "saved" || a.workspace.Requests[0].FormParts[0].Value != "saved" || a.workspace.Requests[0].QueryParams[0].Value != "saved" {
		t.Fatal("复制共享请求切片")
	}
	a.editGroupID = "root"
	a.deleteGroup()
	if len(a.workspace.Groups) != 0 || len(a.workspace.Requests) != 4 || a.workspace.Requests[0].GroupID != "" || a.workspace.Draft.GroupID != "" {
		t.Fatal("删除分组未保留请求或清理草稿关联")
	}
}

func TestGroupsCollectionRemapping(t *testing.T) {
	a, _ := testApp(t)
	makeImport := func() *core.CollectionImport {
		r := appDraft("{{host}}/?q=1")
		r.GroupID = "child"
		r.QueryParams = []core.QueryParam{{ID: "query", Key: "q", Value: "off", Disabled: true}}
		return &core.CollectionImport{Name: "API", Groups: []core.RequestGroup{{ID: "root", Name: "API"}, {ID: "child", Name: "People", ParentID: "root"}}, RootGroupID: "root", Requests: []core.Request{r}}
	}
	first := makeImport()
	a.collectionImport = first
	a.confirmCollectionImport()
	a.collectionImport = makeImport()
	a.confirmCollectionImport()
	if len(a.workspace.Groups) != 4 || len(a.workspace.Requests) != 2 {
		t.Fatal("分组导入失败")
	}
	g := a.workspace.Groups
	if g[0].ID == "root" || g[1].ParentID != g[0].ID || g[3].ParentID != g[2].ID || a.workspace.Requests[0].GroupID != g[1].ID || a.workspace.Requests[1].GroupID != g[3].ID {
		t.Fatal("重复导入的父节点或请求关联没有映射")
	}
	a.workspace.Requests[0].QueryParams[0].Value = "changed"
	if first.Requests[0].QueryParams[0].Value != "off" {
		t.Fatal("导入共享预览参数切片")
	}
	before := appSnapshot(t, a.workspace)
	a.collectionImport = makeImport()
	a.collectionImport.Groups[0].ParentID = "child"
	a.confirmCollectionImport()
	if before != appSnapshot(t, a.workspace) || a.notice == "" {
		t.Fatal("无效组图应拒绝整批导入")
	}
}
