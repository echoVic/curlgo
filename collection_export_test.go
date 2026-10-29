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

func setupCollectionExport(t *testing.T) (*application, chan func()) {
	t.Helper()
	a, q := testApp(t)
	a.workspace.Groups = []core.RequestGroup{{ID: "root", Name: "API"}, {ID: "child", Name: "People", ParentID: "root"}}
	r := appDraft("{{host}}/people?tag=one#anchor")
	r.ID, r.Name, r.GroupID = "saved", "Read", "child"
	r.QueryParams = []core.QueryParam{{ID: "off", Key: "secret", Value: "off", Disabled: true}}
	a.workspace.Requests = []core.Request{r}
	a.workspace.Settings.GlobalHeaders = []core.Pair{{Key: "X-Global", Value: "private-header"}}
	a.workspace.Settings.Environments = map[string]map[string]string{"Local": {"host": "https://private.test"}}
	a.workspace.Settings.Environment = "Local"
	return a, q
}

func TestCollectionExportViewAndSave(t *testing.T) {
	a, q := setupCollectionExport(t)
	path := filepath.Join(t.TempDir(), "saved.json")
	a.pickExportFile = func(opts mygo.SaveDialogOptions) (string, error) {
		if opts.DefaultPath != "API.json" || len(opts.Filters) == 0 {
			t.Errorf("导出保存选项错误：%+v", opts)
		}
		return path, nil
	}
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "导出集合")
	inputUI(t, tt, "集合名称", "API")
	for _, label := range []string{"导出范围", "导出格式", "包含环境与全局请求头", "保存集合文件"} {
		r, ok := tt.Find(label)
		if !ok || r.W < 40 || r.X+r.W > 940 || r.Y+r.H > 680 {
			t.Fatalf("最小窗口导出控件不可用：%s %+v", label, r)
		}
	}
	before := appSnapshot(t, a.workspace)
	clickUI(t, tt, "保存集合文件")
	if !a.exportingCollection {
		t.Fatal("保存期间没有阻止重复点击")
	}
	nextAppUpdate(t, q)()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private.test") || strings.Contains(string(data), "private-header") || !strings.Contains(string(data), "{{host}}") {
		t.Fatal("默认导出丢失模板或带出了环境值与全局请求头")
	}
	parsed, err := core.ImportCurlGoCollection(data)
	if err != nil || len(parsed.Requests) != 1 || !parsed.Requests[0].QueryParams[0].Disabled || len(parsed.Groups) != 2 {
		t.Fatalf("文件无法完整回导：%+v %v", parsed, err)
	}
	if appSnapshot(t, a.workspace) != before || a.exportingCollection {
		t.Fatal("导出改变工作区或未结束")
	}
}

func TestCollectionExportCancellationFailureAndSnapshot(t *testing.T) {
	a, q := setupCollectionExport(t)
	a.openCollectionExport()
	a.pickExportFile = func(mygo.SaveDialogOptions) (string, error) { return "", nil }
	a.chooseCollectionExport()
	nextAppUpdate(t, q)()
	if a.exportingCollection || a.notice != "" {
		t.Fatal("取消保存没有正常返回")
	}
	a.pickExportFile = func(mygo.SaveDialogOptions) (string, error) { return "", errors.New("picker failed") }
	a.chooseCollectionExport()
	nextAppUpdate(t, q)()
	if !strings.Contains(a.notice, "picker failed") || a.page != 5 {
		t.Fatal("保存对话框失败没有保持导出页")
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	picking, release := make(chan struct{}), make(chan struct{})
	a.pickExportFile = func(mygo.SaveDialogOptions) (string, error) {
		close(picking)
		<-release
		return path, nil
	}
	a.exportOptions.IncludeEnvironment = true
	a.exportOptions.GroupID = "child"
	a.chooseCollectionExport()
	<-picking
	a.workspace.Requests[0].Name = "changed"
	a.workspace.Requests[0].QueryParams[0].Value = "changed"
	a.workspace.Settings.Environments["Local"]["host"] = "changed"
	a.exportOptions.Name = "changed"
	a.chooseCollectionExport() // ignored while the first dialog is open
	close(release)
	nextAppUpdate(t, q)()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := core.ImportCurlGoCollection(data)
	if err != nil || got.Requests[0].Name != "Read" || got.Requests[0].QueryParams[0].Value != "off" || got.Environments["Local"]["host"] != "https://private.test" || got.Groups[0].ParentID != "" {
		t.Fatalf("导出过程读取了改变后的工作区：%+v %v", got, err)
	}
	a.pickExportFile = func(mygo.SaveDialogOptions) (string, error) {
		return filepath.Join(t.TempDir(), "missing", "file.json"), nil
	}
	a.chooseCollectionExport()
	nextAppUpdate(t, q)()
	if !strings.Contains(a.notice, "保存失败") || a.exportingCollection {
		t.Fatal("文件写入失败没有反馈")
	}
}

func TestCollectionExportPostmanLimits(t *testing.T) {
	a, _ := setupCollectionExport(t)
	stream := appDraft("https://example.test/events")
	stream.ID, stream.Protocol = "stream", "SSE"
	a.workspace.Requests = append(a.workspace.Requests, stream)
	a.openCollectionExport()
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "导出格式")
	clickUI(t, tt, "Postman v2.1")
	if a.exportOptions.Format != core.CollectionPostman || !tt.HasText("HTTP") || !tt.HasText("超时") || !tt.HasText("Query") || !tt.HasText("1 个请求") {
		t.Fatal("Postman 格式或导出前限制没有显示")
	}
}

func TestCurlGoCollectionPickerImport(t *testing.T) {
	a, q := setupCollectionExport(t)
	result, err := core.ExportCollection(a.workspace, core.CollectionExportOptions{Name: "API", Format: core.CollectionCurlGo, IncludeEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "collection.json")
	if err := os.WriteFile(path, result.Data, 0600); err != nil {
		t.Fatal(err)
	}
	a.pickCollectionFile = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if opts.Directory || !strings.Contains(opts.Title, "CurlGo") {
			t.Error("没有使用 CurlGo 集合选择器")
		}
		return []string{path}, nil
	}
	a.chooseCollection(collectionCurlGo)
	nextAppUpdate(t, q)()
	if a.collectionImport == nil || a.page != 3 {
		t.Fatal("原生集合没有产生导入预览")
	}
	a.confirmCollectionImport()
	if len(a.workspace.Requests) != 2 || len(a.workspace.Groups) != 4 || a.workspace.Requests[1].ID == "saved" ||
		a.workspace.Requests[1].GroupID != a.workspace.Groups[3].ID || a.workspace.Settings.Environment != "Local 2" {
		t.Fatal("原生回导没有重新分配 ID、恢复分组或选择环境")
	}
}
