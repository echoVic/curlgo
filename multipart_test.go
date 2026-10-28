package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestMultipartSnapshots(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Draft = appDraft("https://example.test/upload")
	a.workspace.Draft.BodyMode = core.BodyMultipart
	a.workspace.Draft.FormParts = []core.FormPart{{ID: "text", Kind: core.FormText, Name: "name", Value: "original"}}
	a.saveRequest()
	a.addHistory(a.workspace.Draft, "sent")
	a.workspace.Draft.FormParts[0].Value = "draft-change"
	if a.workspace.Requests[0].FormParts[0].Value != "original" || a.workspace.History[0].Request.FormParts[0].Value != "original" {
		t.Fatal("编辑表单污染了收藏或历史")
	}
	a.selectRequest(a.workspace.Requests[0])
	a.workspace.Draft.FormParts[0].Value = "second-change"
	if a.workspace.Requests[0].FormParts[0].Value != "original" {
		t.Fatal("恢复收藏后编辑表单污染了收藏")
	}
	snapshot := cloneWorkspace(a.workspace)
	before := appSnapshot(t, snapshot)
	a.workspace.Draft.FormParts[0].Value = "changed"
	a.workspace.Requests[0].FormParts[0].Value = "changed"
	a.workspace.History[0].Request.FormParts[0].Value = "changed"
	if appSnapshot(t, snapshot) != before {
		t.Fatal("后台保存快照共享表单内存")
	}
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || appSnapshot(t, got) != before {
		t.Fatalf("重启恢复表单字段失败：%v", err)
	}
}

func TestMultipartFileSelection(t *testing.T) {
	t.Run("选择后自动保存且未读取文件", func(t *testing.T) {
		a, q := testApp(t)
		a.workspace.Draft.BodyMode = core.BodyMultipart
		a.workspace.Draft.FormParts = []core.FormPart{{ID: "upload", Kind: core.FormFile, Name: "file"}}
		path := filepath.Join(t.TempDir(), "not-read-until-send.bin")
		a.pickUploadFile = func(mygo.OpenDialogOptions) ([]string, error) { return []string{path}, nil }
		a.chooseFormFile("upload")
		nextAppUpdate(t, q)()
		if a.workspace.Draft.FormParts[0].FilePath != path || a.revision == 0 {
			t.Fatal("选中的文件路径未保存")
		}
	})
	for _, state := range []string{"切换草稿", "移除字段", "开始发送", "关闭窗口", "取消选择", "选择失败"} {
		t.Run(state, func(t *testing.T) {
			a, q := testApp(t)
			a.workspace.Draft.BodyMode = core.BodyMultipart
			a.workspace.Draft.FormParts = []core.FormPart{{ID: "upload", Kind: core.FormFile, Name: "file"}}
			a.pickUploadFile = func(mygo.OpenDialogOptions) ([]string, error) {
				if state == "取消选择" {
					return nil, nil
				}
				if state == "选择失败" {
					return nil, fmt.Errorf("picker failed")
				}
				return []string{"/tmp/selected.bin"}, nil
			}
			a.chooseFormFile("upload")
			fn := nextAppUpdate(t, q)
			switch state {
			case "切换草稿":
				a.newRequest()
			case "移除字段":
				a.workspace.Draft.FormParts = nil
			case "开始发送":
				a.busy = true
			case "关闭窗口":
				a.closing = true
			}
			before := appSnapshot(t, a.workspace)
			fn()
			if appSnapshot(t, a.workspace) != before {
				t.Fatal("过期或失败的选择结果修改了草稿")
			}
			if state == "选择失败" && a.notice == "" {
				t.Fatal("文件选择失败未提示")
			}
		})
	}
}

func TestMultipartView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(path, []byte("file-payload"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(core.MaxInput); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("attachment")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "file-payload" || header.Filename != "upload.txt" || r.FormValue("title") != "expanded" {
			t.Error("界面输入未完整发送")
		}
		fmt.Fprint(w, "uploaded")
	}))
	defer srv.Close()
	a, q := testApp(t)
	a.pickUploadFile = func(mygo.OpenDialogOptions) ([]string, error) { return []string{path}, nil }
	a.workspace.Settings.Environments[a.workspace.Settings.Environment]["value"] = "expanded"
	a.workspace.Draft = appDraft(srv.URL)
	a.workspace.Draft.Method, a.workspace.Draft.Body = "POST", "retained raw"
	a.syncDraft()
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "正文")
	clickUI(t, tt, "正文类型")
	clickUI(t, tt, "multipart/form-data")
	clickUI(t, tt, "添加文本字段")
	inputUI(t, tt, "字段名称 1", "title")
	inputUI(t, tt, "字段值 1", "{{value}}")
	clickUI(t, tt, "添加文件字段")
	inputUI(t, tt, "字段名称 2", "attachment")
	clickUI(t, tt, "选择文件 2")
	nextAppUpdate(t, q)()
	tt.Frame()
	if !tt.HasText("upload.txt") {
		t.Fatal("未显示选中文件名")
	}
	clickUI(t, tt, "正文类型")
	clickUI(t, tt, "原始文本 / JSON")
	if a.workspace.Draft.Body != "retained raw" {
		t.Fatal("切换正文类型丢失原始正文")
	}
	clickUI(t, tt, "正文类型")
	clickUI(t, tt, "multipart/form-data")
	clickUI(t, tt, "发送  ⌘ ↵")
	pumpApp(t, q, func() bool { return !a.busy })
	tt.Frame()
	if a.response != "uploaded" || a.notice != "" {
		t.Fatalf("界面上传失败：%q", a.notice)
	}
	clickUI(t, tt, "移除字段 2")
	if len(a.workspace.Draft.FormParts) != 1 {
		t.Fatal("移除字段没有生效")
	}
}

func TestMultipartFieldLimit(t *testing.T) {
	a, _ := testApp(t)
	for range core.MaxFormParts + 1 {
		a.addFormPart(core.FormText)
	}
	if len(a.workspace.Draft.FormParts) != core.MaxFormParts || a.notice == "" {
		t.Fatal("字段上限未生效")
	}
}
