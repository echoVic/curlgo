package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportBrunoRequest(t *testing.T) {
	r, warnings, err := parseBrunoRequest(`meta {
  name: Create user
  type: http
  seq: 2
}

post {
  url: {{base_url}}/users/:id?verbose=true
  body: json
  auth: bearer
}

params:path {
  id: 42
}

headers {
  content-type: application/vnd.api+json
  "x-quoted": {{token}}
  ~x-off: off
}

auth:bearer {
  token: {{token}}
}

body:json {
  {
    "a": "}",
    "b": [1, 2]
  }
}

settings {
  followRedirects: false
  timeout: 5000
}
`, brunoContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("不应有提示：%v", warnings)
	}
	if r.Name != "Create user" || r.Method != "POST" || r.URL != "{{base_url}}/users/42?verbose=true" || r.FollowRedirects || r.TimeoutSeconds != 5 {
		t.Fatalf("请求字段错误：%+v", r)
	}
	if r.Body != "{\n  \"a\": \"}\",\n  \"b\": [1, 2]\n}" {
		t.Fatalf("多行正文错误：%q", r.Body)
	}
	want := []Pair{{"content-type", "application/vnd.api+json"}, {"x-quoted", "{{token}}"}}
	if !reflect.DeepEqual(r.Headers, want) || r.Auth != (Authentication{Mode: AuthBearer, Token: "{{token}}"}) {
		t.Fatalf("请求头或认证配置错误：%+v", r)
	}
	wire, err := Resolve(r, Settings{Environment: "test", Environments: map[string]map[string]string{"test": {"base_url": "https://example.com", "token": "resolved"}}})
	if err != nil || HTTPHeaders(wire.Headers).Get("Authorization") != "Bearer resolved" || HTTPHeaders(wire.Headers).Get("x-quoted") != "resolved" {
		t.Fatalf("resolved Bruno headers = %v, error = %v", wire.Headers, err)
	}
}

func TestImportBrunoBodies(t *testing.T) {
	// Bruno multipart fixtures must reference absolute local paths. Build them
	// from temp roots so they are absolute on Windows too ("/abs/..." is not).
	absFile := filepath.Join(t.TempDir(), "file.txt")
	collectionRoot := t.TempDir()
	relativeFile := filepath.Join(collectionRoot, "data", "file.txt")
	for _, tc := range []struct {
		name, block, mode, body, contentType string
		parts                                []FormPart
		warning                              string
	}{
		{name: "text", mode: "text", block: "body:text {\n  hello\n    indented\n}", body: "hello\n  indented", contentType: "text/plain"},
		{name: "xml", mode: "xml", block: "body:xml {\n  <a>{{v}}</a>\n}", body: "<a>{{v}}</a>", contentType: "application/xml"},
		{name: "form", mode: "formUrlEncoded", block: "body:form-urlencoded {\n  a b: x+y&z=\n  token: {{token}}\n  ~off: no\n}", body: "a+b=x%2By%26z%3D&token={{token}}", contentType: "application/x-www-form-urlencoded", warning: "变量"},
		{name: "at-key", mode: "formUrlEncoded", block: "body:form-urlencoded {\n  @description('docs')\n  @field: value\n}", body: "%40field=value", contentType: "application/x-www-form-urlencoded"},
		{name: "multipart", mode: "multipartForm", block: "body:multipart-form {\n  note: {{note}}\n  upload: @file(" + absFile + ")\n  ~off: no\n}", parts: []FormPart{{Kind: FormText, Name: "note", Value: "{{note}}"}, {Kind: FormFile, Name: "upload", FilePath: absFile}}, warning: absFile},
		{name: "relative-file", mode: "multipartForm", block: "body:multipart-form {\n  upload: @file(data/file.txt)\n}", parts: []FormPart{{Kind: FormFile, Name: "upload", FilePath: relativeFile}}, warning: relativeFile},
		{name: "graphql", mode: "graphql", block: "body:graphql {\n  query { me }\n}\n\nbody:graphql:vars {\n  {\"a\": 1}\n}", body: `{"query":"query { me }","variables":{"a": 1}}`, contentType: "application/json"},
		{name: "none", mode: "none", block: "body:json {\n  {\"ignored\": true}\n}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, warnings, err := parseBrunoRequest("meta {\n  name: "+tc.name+"\n}\n\npost {\n  url: https://example.com\n  body: "+tc.mode+"\n}\n\n"+tc.block+"\n", brunoContext{root: collectionRoot})
			if err != nil {
				t.Fatal(err)
			}
			if r.Body != tc.body || HTTPHeaders(r.Headers).Get("Content-Type") != tc.contentType {
				t.Fatalf("正文错误：%q %v", r.Body, r.Headers)
			}
			for i := range r.FormParts {
				r.FormParts[i].ID = ""
			}
			if !reflect.DeepEqual(r.FormParts, tc.parts) || (tc.parts != nil) != (r.BodyMode == BodyMultipart) {
				t.Fatalf("表单错误：%+v", r.FormParts)
			}
			text := strings.Join(warnings, "\n")
			if tc.warning == "" && len(warnings) != 0 || tc.warning != "" && !strings.Contains(text, tc.warning) {
				t.Fatalf("提示错误：%v", warnings)
			}
		})
	}
}

func TestImportBrunoListValuesAndAnnotations(t *testing.T) {
	r, _, err := parseBrunoRequest(`meta {
  name: Tagged
  type: http
  seq: 1
  tags: [
    smoke
    sanity
  ]
}

get {
  url: https://example.com
}

headers {
  @description('''
    Multi-line: docs
  ''')
  x-one: 1
  @description('Single: line')
  x-two: 2
}

assert {
  $res.status: eq 200
}
`, brunoContext{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "Tagged" || !reflect.DeepEqual(r.Headers, []Pair{{"x-one", "1"}, {"x-two", "2"}}) {
		t.Fatalf("标签列表或注解解析错误：%+v", r)
	}
}

func TestImportBrunoMultilineValuesAndQuery(t *testing.T) {
	r, _, err := parseBrunoRequest(`get {
  url: https://example.com/search?existing=1
}

params:query {
  existing: 1
  ~q: disabled
}

headers {
  x-note: '''
    first
    second
  '''
}
`, brunoContext{})
	// A multi-line header value can never be sent, so the request is skipped.
	if err == nil || !strings.Contains(err.Error(), "x-note") {
		t.Fatalf("多行请求头应拒绝导入：%v", err)
	}
	r, _, err = parseBrunoRequest(`post {
  url: https://example.com/search?existing=1
  body: formUrlEncoded
}

params:query {
  existing: 1
}

body:form-urlencoded {
  note: '''
    first
    second
  '''
}
`, brunoContext{})
	if err != nil {
		t.Fatal(err)
	}
	// The url line already carries enabled query params; params:query mirrors it.
	if r.URL != "https://example.com/search?existing=1" || r.Body != "note=first%0Asecond" {
		t.Fatalf("地址或多行表单值错误：%+v", r)
	}
}

func TestImportBrunoAuthInheritance(t *testing.T) {
	collection := brunoContext{auth: brunoAuth{mode: "basic", fields: map[string]string{"username": "ada", "password": "secret"}}}
	inherit := "get {\n  url: https://example.com\n  auth: inherit\n}\n"
	r, _, err := parseBrunoRequest(inherit, collection)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Headers) != 0 || r.Auth != (Authentication{Mode: AuthBasic, Username: "ada", Password: "secret"}) {
		t.Fatalf("集合认证继承错误：%+v", r)
	}
	wire, err := Resolve(r, Settings{})
	if err != nil || HTTPHeaders(wire.Headers).Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("ada:secret")) {
		t.Fatalf("resolved inherited Basic = %v, error = %v", wire.Headers, err)
	}
	folder := collection
	folder.auth = brunoAuth{mode: "none"}
	folder.headers = []Pair{{"authorization", "explicit"}}
	r, _, err = parseBrunoRequest(inherit, folder)
	if err != nil || r.Auth != (Authentication{}) || !reflect.DeepEqual(r.Headers, folder.headers) {
		t.Fatalf("文件夹 auth none 没有覆盖集合认证：%+v %v", r, err)
	}
	wire, err = Resolve(r, Settings{})
	if err != nil || HTTPHeaders(wire.Headers).Get("Authorization") != "explicit" {
		t.Fatalf("none must preserve explicit Authorization: %v %v", wire.Headers, err)
	}
	r, _, err = parseBrunoRequest("get {\n  url: https://example.com\n  auth: basic\n}\n\nauth:basic {\n  username: {{user}}\n  password: {{pass}}\n}\n", collection)
	if err != nil || r.Auth != (Authentication{Mode: AuthBasic, Username: "{{user}}", Password: "{{pass}}"}) || len(r.Headers) != 0 {
		t.Fatalf("Basic variable templates must import without encoding: %+v %v", r, err)
	}
	wire, err = Resolve(r, Settings{Environment: "test", Environments: map[string]map[string]string{"test": {"user": "ada", "pass": "s:cret"}}})
	if err != nil || HTTPHeaders(wire.Headers).Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("ada:s:cret")) {
		t.Fatalf("resolved Basic variables = %v, error = %v", wire.Headers, err)
	}
	if _, _, err := parseBrunoRequest("get {\n  url: https://example.com\n  auth: oauth2\n}\n", brunoContext{}); err == nil {
		t.Error("oauth2 认证不能如实导入，应拒绝")
	}
}

func TestImportBrunoAPIKeyAuth(t *testing.T) {
	for _, tc := range []struct {
		name, placement, key, wireURL string
		location                      APIKeyLocation
	}{
		{"default header", "", "X-Key", "https://example.com/?keep=%2f&api_key=old&api_key=older&flag#top", APIKeyHeader},
		{"header", "headers", "X-Key", "https://example.com/?keep=%2f&api_key=old&api_key=older&flag#top", APIKeyHeader},
		{"query", "queryparams", "api_key", "https://example.com/?keep=%2f&api_key=a%2Bb%26c%3D&flag#top", APIKeyQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "get {\n  url: https://example.com/?keep=%2f&api_key=old&api_key=older&flag#top\n  auth: apikey\n}\n\nauth:apikey {\n  key: {{key_name}}\n  value: {{key}}\n  placement: " + tc.placement + "\n}\n"
			headers := []Pair{{"x-key", "old"}, {"X-KEY", "older"}}
			r, _, err := parseBrunoRequest(source, brunoContext{headers: headers})
			if err != nil {
				t.Fatal(err)
			}
			if r.Auth != (Authentication{Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Location: tc.location}) || !reflect.DeepEqual(r.Headers, headers) || r.URL != "https://example.com/?keep=%2f&api_key=old&api_key=older&flag#top" {
				t.Fatalf("API Key templates changed during import: %+v", r)
			}
			wire, err := Resolve(r, Settings{Environment: "test", Environments: map[string]map[string]string{"test": {"key_name": tc.key, "key": "a+b&c="}}})
			if err != nil || wire.URL != tc.wireURL {
				t.Fatalf("resolved API Key URL = %q, error = %v", wire.URL, err)
			}
			if tc.location == APIKeyHeader && !reflect.DeepEqual(HTTPHeaders(wire.Headers).Values("X-Key"), []string{"a+b&c="}) {
				t.Fatalf("API Key must replace all same-name headers: %v", wire.Headers)
			}
		})
	}
}

func TestImportBrunoRejectsUnsupported(t *testing.T) {
	for name, source := range map[string]string{
		"grpc":         "meta {\n  type: grpc\n}\n\ngrpc {\n  url: x\n}\n",
		"websocket":    "meta {\n  type: ws\n}\n\nws {\n  url: wss://example.com\n}\n",
		"binary":       "post {\n  url: https://example.com\n  body: file\n}\n\nbody:file {\n  file: @file(a.json)\n}\n",
		"sparql":       "post {\n  url: https://example.com\n  body: sparql\n}\n",
		"file-list":    "post {\n  url: https://example.com\n  body: multipartForm\n}\n\nbody:multipart-form {\n  f: @file(/a|/b)\n}\n",
		"part-type":    "post {\n  url: https://example.com\n  body: multipartForm\n}\n\nbody:multipart-form {\n  f: x @contentType(application/json)\n}\n",
		"no-method":    "meta {\n  name: x\n}\n",
		"two-methods":  "get {\n  url: https://a\n}\n\npost {\n  url: https://b\n}\n",
		"unclosed":     "get {\n  url: https://example.com\n",
		"bad-header":   "get {\n  url: https://example.com\n}\n\nheaders {\n  bad name: x\n}\n",
		"ftp":          "get {\n  url: ftp://example.com\n}\n",
		"bad-timeout":  "get {\n  url: https://example.com\n}\n\nsettings {\n  timeout: 5000000\n}\n",
		"unknown-body": "post {\n  url: https://example.com\n  body: magic\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseBrunoRequest(source, brunoContext{}); err == nil {
				t.Fatal("不支持或损坏的请求应拒绝导入")
			}
		})
	}
}

func TestImportBrunoScriptsWarn(t *testing.T) {
	r, warnings, err := parseBrunoRequest("get {\n  url: https://example.com\n}\n\nscript:pre-request {\n  bru.setVar('a', 1)\n}\n\nvars:pre-request {\n  a: 1\n}\n\ntests {\n  test('x', () => {})\n}\n", brunoContext{})
	if err != nil || r.URL != "https://example.com" {
		t.Fatal(err)
	}
	if text := strings.Join(warnings, "\n"); !strings.Contains(text, "脚本") || !strings.Contains(text, "变量") {
		t.Fatalf("脚本和请求变量应提示：%v", warnings)
	}
}

func writeBrunoFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestImportBrunoDirectory(t *testing.T) {
	root := t.TempDir()
	writeBrunoFile(t, root, "bruno.json", `{"version":"1","name":"Shop API","type":"collection"}`)
	writeBrunoFile(t, root, "collection.bru", "headers {\n  x-collection: yes\n}\n\nauth {\n  mode: bearer\n}\n\nauth:bearer {\n  token: {{token}}\n}\n\nvars:pre-request {\n  api: v1\n}\n")
	writeBrunoFile(t, root, "environments/Local.bru", "vars {\n  base_url: http://localhost:8080\n  token: dev\n  ~off: x\n}\n\nvars:secret [\n  password\n]\n")
	writeBrunoFile(t, root, "b-second.bru", "meta {\n  name: Second\n  seq: 2\n}\n\nget {\n  url: {{base_url}}/b\n  auth: inherit\n}\n")
	writeBrunoFile(t, root, "a-first.bru", "meta {\n  name: First\n  seq: 1\n}\n\nget {\n  url: {{base_url}}/a\n  auth: inherit\n}\n")
	writeBrunoFile(t, root, "Orders/folder.bru", "meta {\n  name: 订单\n}\n\nheaders {\n  x-folder: yes\n}\n\nauth {\n  mode: none\n}\n")
	writeBrunoFile(t, root, "Orders/list.bru", "meta {\n  name: List\n}\n\nget {\n  url: {{base_url}}/orders\n  auth: inherit\n}\n\nheaders {\n  x-folder: request\n}\n")
	writeBrunoFile(t, root, "Broken.bru", "meta {\n  name: Broken\n}\n")
	writeBrunoFile(t, root, "notes.txt", "ignored")
	linked := true
	if err := os.Symlink(filepath.Join(root, "a-first.bru"), filepath.Join(root, "link.bru")); err != nil {
		// Creating symlinks requires SeCreateSymbolicLinkPrivilege on Windows;
		// keep the rest of the directory-import coverage without the fixture.
		t.Logf("symlink fixture unavailable: %v", err)
		linked = false
	}
	got, err := ImportBrunoDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Shop API" {
		t.Fatalf("集合名称错误：%q", got.Name)
	}
	var names []string
	for _, r := range got.Requests {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"First", "Second", "List"}) {
		t.Fatalf("请求顺序或文件夹名称错误：%v", names)
	}
	if len(got.Groups) != 2 || GroupPath(got.Groups, got.Requests[2].GroupID) != "Shop API / 订单" || got.Requests[0].GroupID != got.RootGroupID {
		t.Fatalf("Bruno 分组结构丢失：%+v", got.Groups)
	}
	first := got.Requests[0]
	if !reflect.DeepEqual(first.Headers, []Pair{{"x-collection", "yes"}}) || first.Auth != (Authentication{Mode: AuthBearer, Token: "{{token}}"}) {
		t.Fatalf("集合请求头或认证错误：%+v", first)
	}
	list := got.Requests[2]
	if !reflect.DeepEqual(list.Headers, []Pair{{"x-collection", "yes"}, {"x-folder", "request"}}) || list.Auth != (Authentication{}) {
		t.Fatalf("文件夹请求头覆盖或 auth none 错误：%+v", list)
	}
	if !reflect.DeepEqual(got.Environments, map[string]map[string]string{"Local": {"base_url": "http://localhost:8080", "token": "dev", "api": "v1"}}) {
		t.Fatalf("环境错误（集合变量应并入每个环境）：%v", got.Environments)
	}
	settings := Settings{Environment: "Local", Environments: got.Environments}
	wire, err := Resolve(first, settings)
	if err != nil || wire.URL != "http://localhost:8080/a" || HTTPHeaders(wire.Headers).Get("Authorization") != "Bearer dev" {
		t.Fatalf("resolved collection auth = %+v, error = %v", wire, err)
	}
	wire, err = Resolve(list, settings)
	if err != nil || wire.URL != "http://localhost:8080/orders" || HTTPHeaders(wire.Headers).Get("Authorization") != "" {
		t.Fatalf("resolved folder none = %+v, error = %v", wire, err)
	}
	if !warned(got, "Broken.bru") || linked && !warned(got, "link.bru") || !warned(got, "password") {
		t.Fatalf("损坏文件、符号链接和密钥变量应提示：%v", got.Warnings)
	}
}

func TestImportBrunoDirectoryLimits(t *testing.T) {
	root := t.TempDir()
	if _, err := ImportBrunoDirectory(root); err == nil {
		t.Fatal("没有 bruno.json 的目录应拒绝")
	}
	writeBrunoFile(t, root, "bruno.json", `{"name":"Big"}`)
	writeBrunoFile(t, root, "big.bru", "get {\n  url: https://example.com\n}\n"+strings.Repeat("#", MaxInput))
	got, err := ImportBrunoDirectory(root)
	if err != nil || len(got.Requests) != 0 || !warned(got, "big.bru") {
		t.Fatalf("超大文件应跳过并提示：%v %v", err, got.Warnings)
	}
}
