package core

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func postmanCollection(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	fields["info"] = map[string]any{"name": "Example", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func importPostmanItems(t *testing.T, items ...any) CollectionImport {
	t.Helper()
	got, err := ImportPostman(postmanCollection(t, map[string]any{"item": items}))
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func postmanEntry(name string, request any) map[string]any {
	return map[string]any{"name": name, "request": request}
}
func warned(c CollectionImport, parts ...string) bool {
	text := strings.Join(c.Warnings, "\n")
	for _, p := range parts {
		if !strings.Contains(text, p) {
			return false
		}
	}
	return true
}
func requestNamed(t *testing.T, c CollectionImport, name string) Request {
	t.Helper()
	for _, r := range c.Requests {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("没有导入「%s」：%+v 提示=%v", name, c.Requests, c.Warnings)
	return Request{}
}

func TestImportPostmanStructure(t *testing.T) {
	got, err := ImportPostman(postmanCollection(t, map[string]any{
		"variable": []any{
			map[string]any{"key": "base_url", "value": "https://example.com"},
			map[string]any{"key": "port", "value": 8080},
			map[string]any{"key": "off", "value": "x", "disabled": true},
		},
		"item": []any{
			map[string]any{"name": "Users", "item": []any{map[string]any{"name": "Read", "item": []any{
				postmanEntry("List", map[string]any{
					"method": "get",
					"url":    map[string]any{"raw": "{{base_url}}/users/:id/:missing?q=a%20b#frag", "variable": []any{map[string]any{"key": "id", "value": "42"}}},
					"header": []any{map[string]any{"key": "X-On", "value": "{{port}}"}, map[string]any{"key": "X-Off", "value": "off", "disabled": true}},
				}),
			}}}},
			map[string]any{"name": "Ping", "request": "https://example.com/ping", "protocolProfileBehavior": map[string]any{"followRedirects": false}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Example" || len(got.Requests) != 2 {
		t.Fatalf("集合结构错误：%+v", got)
	}
	if !reflect.DeepEqual(got.Environments, map[string]map[string]string{"Example": {"base_url": "https://example.com", "port": "8080"}}) {
		t.Fatalf("集合变量错误：%v", got.Environments)
	}
	list := got.Requests[0]
	if list.Name != "Users / Read / List" || list.Method != "GET" || list.URL != "{{base_url}}/users/42/:missing?q=a%20b#frag" || list.ID == "" || list.Protocol != "HTTP" || list.TimeoutSeconds != 30 {
		t.Fatalf("文件夹、路径变量或原始地址错误：%+v", list)
	}
	if !reflect.DeepEqual(list.Headers, []Pair{{"X-On", "{{port}}"}}) {
		t.Fatalf("请求头错误：%v", list.Headers)
	}
	ping := got.Requests[1]
	if ping.Name != "Ping" || ping.Method != "GET" || ping.URL != "https://example.com/ping" || ping.FollowRedirects {
		t.Fatalf("字符串请求或重定向设置错误：%+v", ping)
	}
}

func TestImportPostmanBodies(t *testing.T) {
	raw := func(text, language string) map[string]any {
		body := map[string]any{"mode": "raw", "raw": text}
		if language != "" {
			body["options"] = map[string]any{"raw": map[string]any{"language": language}}
		}
		return body
	}
	for _, tc := range []struct {
		name, body, contentType string
		source                  map[string]any
		header                  []any
		variableWarning         bool
	}{
		{name: "json", source: raw("{\n \"a\": \"}\"\n}", "json"), body: "{\n \"a\": \"}\"\n}", contentType: "application/json"},
		{name: "xml", source: raw("<x>{{v}}</x>", "xml"), body: "<x>{{v}}</x>", contentType: "application/xml"},
		{name: "text", source: raw(" keep \n", ""), body: " keep \n", contentType: "text/plain"},
		{name: "explicit-type", source: raw("{}", "json"), header: []any{map[string]any{"key": "content-type", "value": "application/vnd.api+json"}}, body: "{}", contentType: "application/vnd.api+json"},
		{name: "urlencoded", source: map[string]any{"mode": "urlencoded", "urlencoded": []any{
			map[string]any{"key": "a b", "value": "x+y&z="},
			map[string]any{"key": "token", "value": "{{token}}"},
			map[string]any{"key": "off", "value": "no", "disabled": true},
		}}, body: "a+b=x%2By%26z%3D&token={{token}}", contentType: "application/x-www-form-urlencoded", variableWarning: true},
		{name: "graphql", source: map[string]any{"mode": "graphql", "graphql": map[string]any{"query": "query { me { id } }", "variables": "{\"id\": 1}"}}, body: `{"query":"query { me { id } }","variables":{"id": 1}}`, contentType: "application/json"},
		{name: "disabled", source: map[string]any{"mode": "raw", "raw": "ignored", "disabled": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := importPostmanItems(t, postmanEntry(tc.name, map[string]any{"method": "POST", "url": "https://example.com", "body": tc.source, "header": tc.header}))
			r := requestNamed(t, got, tc.name)
			if r.Body != tc.body || HTTPHeaders(r.Headers).Get("Content-Type") != tc.contentType || len(HTTPHeaders(r.Headers).Values("Content-Type")) > 1 {
				t.Fatalf("正文或 Content-Type 错误：%q %v", r.Body, r.Headers)
			}
			if tc.variableWarning != warned(got, tc.name, "变量") {
				t.Fatalf("表单变量提示错误：%v", got.Warnings)
			}
		})
	}
}

func TestImportPostmanMultipartReferencesFiles(t *testing.T) {
	got := importPostmanItems(t, postmanEntry("Upload", map[string]any{"method": "POST", "url": "https://example.com", "body": map[string]any{
		"mode": "formdata", "formdata": []any{
			map[string]any{"key": "note", "type": "text", "value": "{{note}}"},
			map[string]any{"key": "upload", "type": "file", "src": "/not-present/import-must-not-read.txt"},
			map[string]any{"key": "off", "type": "file", "src": "relative.txt", "disabled": true},
		},
	}}))
	r := requestNamed(t, got, "Upload")
	if r.BodyMode != BodyMultipart || len(r.FormParts) != 2 || r.Body != "" {
		t.Fatalf("multipart 表单错误：%+v", r)
	}
	note, upload := r.FormParts[0], r.FormParts[1]
	if note.ID == "" || note.Kind != FormText || note.Name != "note" || note.Value != "{{note}}" || upload.Kind != FormFile || upload.FilePath != "/not-present/import-must-not-read.txt" {
		t.Fatalf("表单字段错误：%+v", r.FormParts)
	}
	if !warned(got, "Upload", "/not-present/import-must-not-read.txt") {
		t.Fatalf("没有提示引用的本地文件：%v", got.Warnings)
	}
}

func TestImportPostmanAuth(t *testing.T) {
	auth := func(kind string, fields map[string]any) map[string]any {
		var list []any
		for k, v := range fields {
			list = append(list, map[string]any{"key": k, "value": v, "type": "string"})
		}
		return map[string]any{"type": kind, kind: list}
	}
	got, err := ImportPostman(postmanCollection(t, map[string]any{
		"auth": auth("bearer", map[string]any{"token": "{{token}}"}),
		"item": []any{
			map[string]any{"name": "Open", "auth": map[string]any{"type": "noauth"}, "item": []any{postmanEntry("Public", map[string]any{"method": "GET", "url": "https://example.com/public"})}},
			map[string]any{"name": "Inherit", "item": []any{postmanEntry("Private", map[string]any{"method": "GET", "url": "https://example.com/private"})}},
			postmanEntry("Basic", map[string]any{"method": "GET", "url": "https://example.com", "auth": auth("basic", map[string]any{"username": "ada", "password": "s:cret"})}),
			postmanEntry("BasicVar", map[string]any{"method": "GET", "url": "https://example.com", "auth": auth("basic", map[string]any{"username": "{{user}}", "password": "x"})}),
			postmanEntry("QueryKey", map[string]any{"method": "GET", "url": "https://example.com/a?q=1#top", "auth": auth("apikey", map[string]any{"key": "api_key", "value": "{{key}}", "in": "query"})}),
			postmanEntry("HeaderKey", map[string]any{"method": "GET", "url": "https://example.com", "auth": auth("apikey", map[string]any{"key": "X-Api-Key", "value": "{{key}}"})}),
			postmanEntry("Replace", map[string]any{"method": "GET", "url": "https://example.com", "header": []any{map[string]any{"key": "authorization", "value": "old"}}}),
			postmanEntry("OAuth", map[string]any{"method": "GET", "url": "https://example.com", "auth": auth("oauth2", map[string]any{"accessToken": "x"})}),
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	authorization := func(name string) []string {
		return HTTPHeaders(requestNamed(t, got, name).Headers).Values("Authorization")
	}
	if len(authorization("Open / Public")) != 0 || !reflect.DeepEqual(authorization("Inherit / Private"), []string{"Bearer {{token}}"}) {
		t.Fatal("文件夹认证继承或 noauth 错误")
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ada:s:cret")); !reflect.DeepEqual(authorization("Basic"), []string{want}) {
		t.Fatalf("Basic 认证错误：%v", authorization("Basic"))
	}
	if !reflect.DeepEqual(authorization("Replace"), []string{"Bearer {{token}}"}) {
		t.Fatalf("认证没有替换原 Authorization：%v", authorization("Replace"))
	}
	if r := requestNamed(t, got, "QueryKey"); r.URL != "https://example.com/a?q=1&api_key={{key}}#top" {
		t.Fatalf("查询参数 API Key 错误：%s", r.URL)
	}
	if HTTPHeaders(requestNamed(t, got, "HeaderKey").Headers).Get("X-Api-Key") != "{{key}}" {
		t.Fatal("请求头 API Key 错误")
	}
	if len(got.Requests) != 6 || !warned(got, "BasicVar", "OAuth", "oauth2") {
		t.Fatalf("不能如实导入的认证应跳过并提示：%d %v", len(got.Requests), got.Warnings)
	}
}

func TestImportPostmanSkipsUnsupportedRequests(t *testing.T) {
	file := func(src any, extra map[string]any) map[string]any {
		part := map[string]any{"key": "f", "type": "file", "src": src}
		for k, v := range extra {
			part[k] = v
		}
		return map[string]any{"mode": "formdata", "formdata": []any{part}}
	}
	cases := map[string]map[string]any{
		"binary-body":       {"body": map[string]any{"mode": "file", "file": map[string]any{"src": "/secret"}}},
		"relative-file":     {"body": file("secret.txt", nil)},
		"empty-file":        {"body": file(nil, nil)},
		"file-list":         {"body": file([]string{"/a", "/b"}, nil)},
		"part-content-type": {"body": file("/a", map[string]any{"contentType": "application/json"})},
		"bad-graphql-vars":  {"body": map[string]any{"mode": "graphql", "graphql": map[string]any{"query": "q", "variables": "{bad"}}},
		"ftp":               {"url": "ftp://example.com"},
		"no-url":            {"url": ""},
		"invalid-header":    {"header": []any{map[string]any{"key": "X", "value": "a\nb"}}},
		"invalid-method":    {"method": "GET X"},
	}
	items := []any{postmanEntry("Good", map[string]any{"method": "GET", "url": "https://example.com"})}
	for name, request := range cases {
		if _, ok := request["method"]; !ok {
			request["method"] = "POST"
		}
		if _, ok := request["url"]; !ok {
			request["url"] = "https://example.com"
		}
		items = append(items, postmanEntry(name, request))
	}
	got := importPostmanItems(t, items...)
	if len(got.Requests) != 1 || got.Requests[0].Name != "Good" {
		t.Fatalf("不支持的请求被导入：%+v", got.Requests)
	}
	for name := range cases {
		if !warned(got, "「"+name+"」") {
			t.Errorf("跳过 %s 时没有提示：%v", name, got.Warnings)
		}
	}
}

func TestImportPostmanScriptsAreNotRun(t *testing.T) {
	script := []any{map[string]any{"listen": "prerequest", "script": map[string]any{"exec": []string{"pm.environment.set('x', 1)"}}}}
	got, err := ImportPostman(postmanCollection(t, map[string]any{
		"event": script,
		"item": []any{
			map[string]any{"name": "Scripted", "event": []any{map[string]any{"listen": "test", "script": map[string]any{"exec": []string{"pm.test('ok')"}}}}, "request": map[string]any{"method": "GET", "url": "https://example.com"}},
			map[string]any{"name": "Empty", "event": []any{map[string]any{"listen": "test", "script": map[string]any{"exec": []string{""}}}}, "request": map[string]any{"method": "GET", "url": "https://example.com"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Requests) != 2 || !warned(got, "脚本", "Example", "Scripted") || warned(got, "Empty") {
		t.Fatalf("脚本应提示且不执行：%v", got.Warnings)
	}
}

func TestImportPostmanRejectsOtherFormats(t *testing.T) {
	for name, data := range map[string]string{
		"无效JSON": "{",
		"v1集合":   `{"id":"x","name":"old","requests":[]}`,
		"v2.0集合": `{"info":{"name":"x","schema":"https://schema.getpostman.com/json/collection/v2.0.0/collection.json"},"item":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ImportPostman([]byte(data)); err == nil {
				t.Fatal("非 Collection v2.1 文件应拒绝导入")
			} else if name != "无效JSON" && !strings.Contains(err.Error(), "v2.1") {
				t.Fatalf("错误没有说明支持的格式：%v", err)
			}
		})
	}
}
