package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func uploadFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEngineMultipart(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 255, 13, 10}, 300000)
	name := `中文 "文件".bin`
	if runtime.GOOS == "windows" {
		// Windows forbids double quotes in file names; filename quote escaping
		// in the multipart wire format is still covered on POSIX hosts.
		name = "中文 文件.bin"
	}
	path := uploadFile(t, name, data)
	type received struct {
		names, values, filenames []string
		hash                     [32]byte
		declared, actual         int64
	}
	seen := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mt, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
			t.Errorf("请求未携带有效的 multipart boundary：%q", req.Header.Get("Content-Type"))
			w.WriteHeader(400)
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			return
		}
		got := received{declared: req.ContentLength, actual: int64(len(body))}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			value, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
				return
			}
			got.names = append(got.names, part.FormName())
			got.filenames = append(got.filenames, part.FileName())
			if part.FileName() != "" {
				got.hash = sha256.Sum256(value)
				got.values = append(got.values, "file")
			} else {
				got.values = append(got.values, string(value))
			}
		}
		seen <- got
		fmt.Fprint(w, `{"uploaded":true}`)
	}))
	defer srv.Close()
	r := engineRequest(srv.URL)
	r.Method, r.BodyMode, r.Body = "POST", BodyMultipart, "inactive body"
	r.FormParts = []FormPart{
		{Kind: FormText, Name: "title", Value: "中文\r\n第二行"},
		{Kind: FormText, Name: "tag", Value: "one"},
		{Kind: FormText, Name: "tag", Value: "two"},
		{Kind: FormText, Name: "empty", Value: ""},
		{Kind: FormFile, Name: "attachment", FilePath: path},
	}
	r.Headers = []Pair{{"Content-Type", "application/json"}}
	events, err := collectRun(engineFixture(t), context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Status != "200 OK" {
		t.Fatalf("上传失败：%+v", events)
	}
	got := <-seen
	if !reflect.DeepEqual(got.names, []string{"title", "tag", "tag", "empty", "attachment"}) ||
		!reflect.DeepEqual(got.values, []string{"中文\r\n第二行", "one", "two", "", "file"}) ||
		got.filenames[4] != filepath.Base(path) || got.hash != sha256.Sum256(data) ||
		got.declared != got.actual {
		t.Fatalf("multipart 上传内容或 Content-Length 错误：%+v", got)
	}
}

func TestEngineMultipartFileErrors(t *testing.T) {
	for name, path := range map[string]string{
		"文件已删除": filepath.Join(t.TempDir(), "missing.txt"),
		"目录":    t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			e := engineFixture(t)
			r := engineRequest("http://127.0.0.1:1")
			r.BodyMode = BodyMultipart
			r.FormParts = []FormPart{{Kind: FormFile, Name: "file", FilePath: path}}
			events, err := collectRun(e, context.Background(), r)
			if err == nil || !strings.Contains(err.Error(), "文件") || len(events) != 0 {
				t.Fatalf("文件问题未在请求前报告：%v", err)
			}
		})
	}
}

func TestEngineMultipartMultipleAndEmptyFiles(t *testing.T) {
	paths := []string{uploadFile(t, "empty.txt", nil), uploadFile(t, "data.txt", []byte("payload"))}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseMultipartForm(MaxInput); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer req.MultipartForm.RemoveAll()
		files := req.MultipartForm.File["files"]
		if len(files) != 2 {
			t.Errorf("同名文件字段丢失：%+v", files)
			w.WriteHeader(400)
			return
		}
		for i, header := range files {
			file, err := header.Open()
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(file)
			file.Close()
			expected := ""
			if i == 1 {
				expected = "payload"
			}
			if err != nil || string(data) != expected || header.Filename != filepath.Base(paths[i]) {
				t.Errorf("文件内容或文件名丢失：%v %+v", err, header)
			}
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	r := engineRequest(srv.URL)
	r.Method, r.BodyMode = "POST", BodyMultipart
	for _, path := range paths {
		r.FormParts = append(r.FormParts, FormPart{Kind: FormFile, Name: "files", FilePath: path})
	}
	if _, err := collectRun(engineFixture(t), context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestMultipartFileReadersClose(t *testing.T) {
	path := uploadFile(t, "data.txt", []byte("payload"))
	body, _, err := openMultipart([]FormPart{{Kind: FormFile, Name: "file", FilePath: path}}, "boundary")
	if err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := body.files[0].Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("关闭上传正文后文件未关闭：%v", err)
	}
}

func TestEngineMultipartRedirect(t *testing.T) {
	path := uploadFile(t, "data.txt", []byte("payload"))
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/" {
					io.Copy(io.Discard, req.Body)
					http.Redirect(w, req, "/target", status)
					return
				}
				if err := req.ParseMultipartForm(MaxInput); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer req.MultipartForm.RemoveAll()
				file, _, err := req.FormFile("file")
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()
				data, _ := io.ReadAll(file)
				fmt.Fprint(w, string(data))
			}))
			defer srv.Close()
			r := engineRequest(srv.URL)
			r.Method, r.BodyMode, r.FollowRedirects = "POST", BodyMultipart, true
			r.FormParts = []FormPart{{Kind: FormFile, Name: "file", FilePath: path}}
			events, err := collectRun(engineFixture(t), context.Background(), r)
			if err != nil || bodyEvent(t, events).Text != "payload" {
				t.Fatalf("重定向丢失上传正文：%v", err)
			}
		})
	}
}

func TestEngineMultipartCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	file.Close()
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-release
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := engineRequest(srv.URL)
	r.Method, r.BodyMode = "POST", BodyMultipart
	r.FormParts = []FormPart{{Kind: FormFile, Name: "file", FilePath: path}}
	done := make(chan error, 1)
	e := engineFixture(t)
	go func() { done <- e.Run(ctx, r, func(Event) {}) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("上传未开始")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("上传取消错误未保留：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("上传取消后仍阻塞")
	}
}
