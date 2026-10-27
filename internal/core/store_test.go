package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testWorkspace() Workspace {
	w := NewWorkspace()
	w.Draft = modelRequest()
	w.Requests = []Request{modelRequest()}
	w.Settings.GlobalHeaders = []Pair{{"X-Global", "value"}}
	w.History = []History{{Request: modelRequest(), At: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Status: "201 Created"}}
	return w
}
func writeStoreFixture(t *testing.T, path string, w Workspace) {
	t.Helper()
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func assertStoreMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != mode {
		t.Fatalf("%s 权限=%o，期望=%o", filepath.Base(path), st.Mode().Perm(), mode)
	}
}

func TestOpenStore(t *testing.T) {
	t.Run("创建私有目录", func(t *testing.T) { s := openTestStore(t); assertStoreMode(t, s.Dir, 0700) })
	t.Run("已有目录权限收紧", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "existing")
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenStore(dir); err != nil {
			t.Fatal(err)
		}
		assertStoreMode(t, dir, 0700)
	})
	t.Run("路径被普通文件占用", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := OpenStore(path)
		if err == nil || s != nil {
			t.Fatal("目录不可创建时应返回错误")
		}
		b, _ := os.ReadFile(path)
		if string(b) != "sentinel" {
			t.Fatal("覆盖了已有文件")
		}
	})
}

func TestStoreLoad(t *testing.T) {
	t.Run("首次加载无写入", func(t *testing.T) {
		s := openTestStore(t)
		w, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		if w.Version <= 0 || w.Draft.ID == "" {
			t.Fatal("缺少可编辑初始草稿")
		}
		files, err := os.ReadDir(s.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 0 {
			t.Fatal("首次读取产生了持久化副作用")
		}
	})
	t.Run("有效工作区逐字段恢复", func(t *testing.T) {
		s := openTestStore(t)
		want := testWorkspace()
		writeStoreFixture(t, filepath.Join(s.Dir, "workspace.json"), want)
		got, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("持久化字段丢失：got=%+v want=%+v", got, want)
		}
	})
	t.Run("损坏文件从备份恢复并保留原件", func(t *testing.T) {
		s := openTestStore(t)
		path := filepath.Join(s.Dir, "workspace.json")
		broken := []byte("{broken-primary")
		if err := os.WriteFile(path, broken, 0600); err != nil {
			t.Fatal(err)
		}
		want := testWorkspace()
		writeStoreFixture(t, path+".bak", want)
		got, err := s.Load()
		requireModelError(t, err, "恢复", "备份")
		if !reflect.DeepEqual(got, want) {
			t.Fatal("没有返回备份数据")
		}
		files, _ := filepath.Glob(path + ".corrupt-*")
		if len(files) != 1 {
			t.Fatalf("未保留唯一原件：%v", files)
		}
		data, _ := os.ReadFile(files[0])
		if !bytes.Equal(data, broken) {
			t.Fatal("损坏原件内容被改变")
		}
		if _, err := os.Stat(path + ".bak"); err != nil {
			t.Fatal("备份被删除")
		}
	})
	t.Run("主文件缺失仍恢复有效备份", func(t *testing.T) {
		s := openTestStore(t)
		want := testWorkspace()
		writeStoreFixture(t, filepath.Join(s.Dir, "workspace.json.bak"), want)
		got, err := s.Load()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("有效备份被忽略并重置：%+v err=%v", got, err)
		}
		requireModelError(t, err, "恢复", "备份")
	})
	t.Run("不可恢复保留原文件", func(t *testing.T) {
		s := openTestStore(t)
		path := filepath.Join(s.Dir, "workspace.json")
		broken := []byte("broken")
		if err := os.WriteFile(path, broken, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := s.Load()
		requireModelError(t, err, "保留", "读取")
		b, _ := os.ReadFile(path)
		if !bytes.Equal(b, broken) {
			t.Fatal("不可恢复文件被修改")
		}
	})
	t.Run("未知版本不静默重置", func(t *testing.T) {
		s := openTestStore(t)
		w := testWorkspace()
		w.Version = 999
		path := filepath.Join(s.Dir, "workspace.json")
		writeStoreFixture(t, path, w)
		before, _ := os.ReadFile(path)
		_, err := s.Load()
		requireModelError(t, err, "版本")
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("未知版本被覆盖")
		}
	})
	t.Run("超大文件拒绝且保留", func(t *testing.T) {
		s := openTestStore(t)
		path := filepath.Join(s.Dir, "workspace.json")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate((16 << 20) + 1); err != nil {
			t.Fatal(err)
		}
		f.Close()
		_, err = s.Load()
		requireModelError(t, err, "16 MiB", "超过")
		st, err := os.Stat(path)
		if err != nil || st.Size() != (16<<20)+1 {
			t.Fatal("超大原件没有保留")
		}
	})
}

func TestStoreSave(t *testing.T) {
	t.Run("保存字段与文件权限", func(t *testing.T) {
		s := openTestStore(t)
		want := testWorkspace()
		before := modelSnapshot(t, want)
		if err := s.Save(want); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Dir, "workspace.json")
		assertStoreMode(t, path, 0600)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got Workspace
		if err = json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("保存遗漏字段：%+v", got)
		}
		if modelSnapshot(t, want) != before {
			t.Fatal("保存修改了输入工作区")
		}
		var raw map[string]json.RawMessage
		if err = json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw["history"], []byte(`"response"`)) {
			t.Fatal("历史持久化包含响应内容")
		}
	})
	t.Run("保留上一份有效备份", func(t *testing.T) {
		s := openTestStore(t)
		a, b := testWorkspace(), testWorkspace()
		a.Draft.Name = "old"
		b.Draft.Name = "new"
		if err := s.Save(a); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(b); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Dir, "workspace.json.bak")
		assertStoreMode(t, path, 0600)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got Workspace
		if err = json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, a) {
			t.Fatal("备份不是上一份有效数据")
		}
		current, err := s.Load()
		if err != nil || !reflect.DeepEqual(current, b) {
			t.Fatal("新数据未持久化")
		}
	})
	t.Run("损坏原件不覆盖", func(t *testing.T) {
		s := openTestStore(t)
		path := filepath.Join(s.Dir, "workspace.json")
		if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
		err := s.Save(testWorkspace())
		requireModelError(t, err, "损坏", "覆盖")
		data, _ := os.ReadFile(path)
		if string(data) != "broken" {
			t.Fatal("保存覆盖损坏证据")
		}
	})
	t.Run("超大内容拒绝保留已有数据", func(t *testing.T) {
		s := openTestStore(t)
		w := testWorkspace()
		if err := s.Save(w); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Dir, "workspace.json")
		before, _ := os.ReadFile(path)
		w.Draft.Body = strings.Repeat("x", 16<<20)
		err := s.Save(w)
		requireModelError(t, err, "16 MiB", "超过")
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("失败保存修改了原件")
		}
	})
	t.Run("无效版本不落盘", func(t *testing.T) {
		s := openTestStore(t)
		w := testWorkspace()
		w.Version = 999
		err := s.Save(w)
		requireModelError(t, err, "版本")
		if _, err := os.Stat(filepath.Join(s.Dir, "workspace.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("无效版本写入文件")
		}
	})
	t.Run("备份失败不得覆盖主文件", func(t *testing.T) {
		s := openTestStore(t)
		w := testWorkspace()
		if err := s.Save(w); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Dir, "workspace.json")
		before, _ := os.ReadFile(path)
		if err := os.Mkdir(path+".bak", 0700); err != nil {
			t.Fatal(err)
		}
		w.Draft.Name = "new"
		if err := s.Save(w); err == nil {
			t.Fatal("备份失败应报告错误")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("备份失败仍覆盖主文件")
		}
		temps, _ := filepath.Glob(filepath.Join(s.Dir, ".qingzhou-*"))
		if len(temps) != 0 {
			t.Fatal("失败写入遗留临时文件")
		}
	})
	t.Run("并发保存形成完整版本", func(t *testing.T) {
		s := openTestStore(t)
		a, b := testWorkspace(), testWorkspace()
		a.Draft.Name = "a"
		b.Draft.Name = "b"
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, w := range []Workspace{a, b} {
			wg.Add(1)
			go func(w Workspace) { defer wg.Done(); <-start; errs <- s.Save(w) }(w)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, a) && !reflect.DeepEqual(got, b) {
			t.Fatal("并发写入产生混合数据")
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, "workspace.json.bak"))
		if err != nil {
			t.Fatal(err)
		}
		var backup Workspace
		if err = json.Unmarshal(data, &backup); err != nil {
			t.Fatal(err)
		}
		if got.Draft.Name == backup.Draft.Name || backup.Draft.Name != "a" && backup.Draft.Name != "b" {
			t.Fatal("主备没有形成两个完整版本")
		}
	})
}

func TestAtomicWrite(t *testing.T) {
	t.Run("替换现有文件并设私有权限", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data")
		if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, []byte("new\x00binary")); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "new\x00binary" {
			t.Fatal("内容不完整")
		}
		assertStoreMode(t, path, 0600)
	})
	t.Run("空内容可保存", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty")
		if err := atomicWrite(path, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) != 0 {
			t.Fatal("空文件未保存")
		}
	})
	t.Run("目标是目录时清理临时文件", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "target")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, []byte("data")); err == nil {
			t.Fatal("应报告替换失败")
		}
		files, _ := os.ReadDir(dir)
		if len(files) != 1 || files[0].Name() != "target" {
			t.Fatalf("失败后遗留临时文件：%v", files)
		}
	})
}
