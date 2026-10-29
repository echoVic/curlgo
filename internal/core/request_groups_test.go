package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestGroups(t *testing.T) {
	groups := []RequestGroup{{ID: "root", Name: "API"}, {ID: "child", Name: "Users / Admin", ParentID: "root"}, {ID: "other", Name: "API"}}
	if err := ValidateRequestGroups(groups); err != nil {
		t.Fatal(err)
	}
	if GroupPath(groups, "child") != "API / Users / Admin" || GroupPath(groups, "missing") != "未分组" {
		t.Fatal("分组路径错误")
	}
	ids := GroupSubtree(groups, "root")
	if !ids["root"] || !ids["child"] || ids["other"] || len(GroupSubtree(groups, "missing")) != 0 {
		t.Fatal("分组后代范围错误")
	}
	for name, invalid := range map[string][]RequestGroup{
		"duplicate": {{ID: "a", Name: "A"}, {ID: "a", Name: "B"}},
		"missing":   {{ID: "a", Name: "A", ParentID: "missing"}},
		"cycle":     {{ID: "a", Name: "A", ParentID: "b"}, {ID: "b", Name: "B", ParentID: "a"}},
		"empty":     {{ID: "a", Name: " "}},
		"control":   {{ID: "a", Name: "Bad\nName"}},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateRequestGroups(invalid) == nil {
				t.Fatal("无效分组结构没有被拒绝")
			}
		})
	}
	var deep []RequestGroup
	parent := ""
	for i := range MaxGroupDepth + 1 {
		id := strings.Repeat("x", i+1)
		deep = append(deep, RequestGroup{ID: id, Name: id, ParentID: parent})
		parent = id
	}
	if ValidateRequestGroups(deep) == nil {
		t.Fatal("无界层级没有被拒绝")
	}
}

func TestRequestGroupPaths(t *testing.T) {
	groups := []RequestGroup{{ID: "child", Name: "Users", ParentID: "root"}, {ID: "other", Name: "Users"}, {ID: "root", Name: "API"}}
	paths := GroupPaths(groups)
	if len(paths) != 3 || paths["root"] != "API" || paths["child"] != "API / Users" || paths["other"] != "Users" {
		t.Fatalf("按 ID 构建路径失败：%v", paths)
	}
}

func TestGroupsWorkspace(t *testing.T) {
	old := `{"version":1,"draft":{"id":"draft"},"requests":[{"id":"legacy","name":"Health"}],"settings":{}}`
	w, err := decodeWorkspace([]byte(old))
	if err != nil || len(w.Groups) != 0 || w.Requests[0].GroupID != "" {
		t.Fatalf("旧工作区不兼容：%v", err)
	}
	w.Groups = []RequestGroup{{ID: "root", Name: "API"}, {ID: "child", Name: "Users", ParentID: "root"}}
	w.Requests[0].GroupID = "child"
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(w); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.Requests[0].GroupID != "child" || got.Groups[1].ParentID != "root" {
		t.Fatalf("存储丢失分组结构：%+v %v", got, err)
	}
	w.Groups[0].ParentID = "child"
	data, _ := json.Marshal(w)
	if _, err := decodeWorkspace(data); err == nil {
		t.Fatal("读取边界应拒绝循环分组")
	}
}
