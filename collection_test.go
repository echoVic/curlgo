package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestApplicationCollectionImport(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Settings.Environments["Imported"] = map[string]string{"existing": "keep"}
	imported := appDraft("{{host}}/api")
	imported.ID, imported.Name = "imported", "Imported / Health"
	a.collectionImport = &core.CollectionImport{
		Name: "Imported", Requests: []core.Request{imported},
		Environments: map[string]map[string]string{"Imported": {"host": "https://example.test"}, "Staging": {"host": "https://staging.test"}},
		Warnings:     []string{"已跳过「Upload」：二进制文件正文暂不支持"},
	}
	original := appSnapshot(t, a.workspace)
	a.page = 3
	tt := ui.NewTester(a.view, 940, 680)
	if !tt.HasText("已跳过「Upload」") || !tt.HasText("Imported / Health") || appSnapshot(t, a.workspace) != original {
		t.Fatal("导入预览未显示请求和限制，或提前更改了工作区")
	}
	clickUI(t, tt, "加入收藏")
	if len(a.workspace.Requests) != 1 || a.workspace.Requests[0].ID == "imported" || a.busy || len(a.workspace.History) != 0 || a.page != 0 || a.collectionImport != nil {
		t.Fatal("导入应以新 ID 加入收藏、回到请求页且不发送")
	}
	envs := a.workspace.Settings.Environments
	if envs["Imported"]["existing"] != "keep" || envs["Imported"]["host"] != "" {
		t.Fatal("导入覆盖了同名的现有环境")
	}
	if envs["Staging"]["host"] != "https://staging.test" || envs["Imported 2"]["host"] != "https://example.test" {
		t.Fatalf("导入的环境没有以不冲突的名称加入：%v", envs)
	}
	if a.workspace.Settings.Environment != "Imported 2" {
		t.Fatalf("应切换到集合同名的导入环境：%q", a.workspace.Settings.Environment)
	}
	a.workspace.Draft = a.workspace.Requests[0]
	if r, err := core.Resolve(a.workspace.Draft, a.workspace.Settings); err != nil || r.URL != "https://example.test/api" {
		t.Fatalf("导入后的变量没有用于请求：%v", err)
	}
	a.collectionImport = &core.CollectionImport{Name: "Imported", Requests: []core.Request{imported}}
	a.confirmCollectionImport()
	if len(a.workspace.Requests) != 2 || a.workspace.Requests[0].ID == a.workspace.Requests[1].ID {
		t.Fatal("重复导入应生成独立的收藏")
	}
}

func TestCollectionImportEmptyAndLimits(t *testing.T) {
	a, _ := testApp(t)
	a.page = 3
	a.collectionImport = &core.CollectionImport{Name: "Empty", Warnings: []string{"已跳过「Socket」：暂不支持 ws 类型的请求"}}
	tt := ui.NewTester(a.view, 1200, 820)
	if !tt.HasText("没有可导入的请求") || !tt.HasText("已跳过「Socket」") {
		t.Fatal("空导入应说明原因")
	}
	a.workspace.Requests = make([]core.Request, 4999)
	a.collectionImport = &core.CollectionImport{Name: "Big", Requests: []core.Request{appDraft("https://a.test"), appDraft("https://b.test")}}
	a.confirmCollectionImport()
	if len(a.workspace.Requests) != 4999 || !strings.Contains(a.notice, "5000") {
		t.Fatal("超过收藏上限时应拒绝整批导入")
	}
}

func TestCollectionPickerPreviewAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.json")
	data := `{"info":{"name":"Test","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"Health","request":{"method":"GET","url":"https://example.test/health"}}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	a, q := testApp(t)
	a.page = 2
	a.pickCollectionFile = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if opts.Directory || len(opts.Filters) == 0 {
			t.Error("Postman 文件选择器选项错误")
		}
		return []string{path}, nil
	}
	a.chooseCollection(collectionPostman)
	if !a.importingCollection {
		t.Fatal("选择期间应阻止重复打开")
	}
	nextAppUpdate(t, q)()
	if a.collectionImport == nil || a.page != 3 || len(a.workspace.Requests) != 0 || a.importingCollection {
		t.Fatal("文件解析应产生预览而不是直接导入")
	}
	tt := ui.NewTester(a.view, 1200, 820)
	clickUI(t, tt, "取消导入")
	if a.collectionImport != nil || len(a.workspace.Requests) != 0 || a.page != 2 {
		t.Fatal("取消导入修改了工作区")
	}
	a.pickCollectionFile = func(mygo.OpenDialogOptions) ([]string, error) { return nil, errors.New("picker failed") }
	a.chooseCollection(collectionPostman)
	nextAppUpdate(t, q)()
	if !strings.Contains(a.notice, "picker failed") || a.collectionImport != nil {
		t.Fatal("选择失败没有保持现有工作区")
	}
	a.pickCollectionFile = func(mygo.OpenDialogOptions) ([]string, error) { return nil, nil }
	a.notice = ""
	a.chooseCollection(collectionPostman)
	nextAppUpdate(t, q)()
	if a.notice != "" || a.collectionImport != nil || a.importingCollection {
		t.Fatal("取消选择不应提示错误")
	}
}

func TestCollectionPickerBrunoFolder(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"bruno.json": `{"name":"Bru"}`,
		"ping.bru":   "meta {\n  name: Ping\n}\n\nget {\n  url: https://example.test/ping\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, q := testApp(t)
	a.pickCollectionFile = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if !opts.Directory {
			t.Error("Bruno 应选择集合文件夹")
		}
		return []string{root}, nil
	}
	a.chooseCollection(collectionBruno)
	nextAppUpdate(t, q)()
	if a.collectionImport == nil || len(a.collectionImport.Requests) != 1 || a.collectionImport.Requests[0].Name != "Ping" {
		t.Fatalf("Bruno 文件夹没有解析为预览：%+v %q", a.collectionImport, a.notice)
	}
}

func TestCollectionImportButtonsFitMinimumWindow(t *testing.T) {
	a, _ := testApp(t)
	a.page = 2
	tt := ui.NewTester(a.view, 940, 680)
	for _, label := range []string{"Postman 集合", "Bruno 集合", "导入请求"} {
		r, ok := tt.Find(label)
		if !ok || r.X+r.W > 940 || r.Y+r.H > 680 {
			t.Fatalf("最小窗口内看不到「%s」：%+v", label, r)
		}
	}
}

func TestReadCollectionFileLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxCollectionFile+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCollection(collectionPostman, path); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("超大集合文件应拒绝：%v", err)
	}
}
