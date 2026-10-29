package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportPostmanGroups(t *testing.T) {
	got := importPostmanItems(t,
		map[string]any{"name": "Names / Stay", "item": []any{postmanEntry("Leaf / Name", "https://example.test")}},
		map[string]any{"name": "Empty", "item": []any{}},
		map[string]any{"name": "Other", "item": []any{map[string]any{"name": "Names / Stay", "item": []any{}}}},
	)
	if len(got.Groups) != 5 || len(got.Requests) != 1 || got.Requests[0].Name != "Leaf / Name" {
		t.Fatalf("空目录或原始名称丢失：%+v", got)
	}
	if got.Groups[1].Name != "Names / Stay" || got.Groups[1].ParentID != got.RootGroupID || got.Requests[0].GroupID != got.Groups[1].ID {
		t.Fatal("名称中的斜线被错误当作层级")
	}
	if err := ValidateRequestGroups(got.Groups); err != nil {
		t.Fatal(err)
	}
}

func TestImportBrunoDirectoryGroups(t *testing.T) {
	root := t.TempDir()
	writeBrunoFile(t, root, "bruno.json", `{"name":"Bruno"}`)
	writeBrunoFile(t, root, "Orders/folder.bru", "meta {\n  name: Names / Stay\n}\n")
	writeBrunoFile(t, root, "Orders/leaf.bru", "meta {\n  name: Leaf / Name\n}\nget {\n  url: https://example.test\n}\n")
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := ImportBrunoDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 3 || len(got.Requests) != 1 || got.Requests[0].Name != "Leaf / Name" || GroupPath(got.Groups, got.Requests[0].GroupID) != "Bruno / Names / Stay" {
		t.Fatalf("导入层级或叶名丢失：%+v", got)
	}
}
