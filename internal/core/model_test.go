package core

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func modelRequest() Request {
	return Request{ID: "draft-id", Name: "示例", Protocol: "HTTP", Method: "POST", URL: "https://example.test/api", Body: "正文", FollowRedirects: true, TimeoutSeconds: 12}
}

func modelSnapshot(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func requireModelError(t *testing.T, err error, clues ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("期望明确错误，实际成功")
	}
	for _, clue := range clues {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(clue)) {
			return
		}
	}
	t.Fatalf("错误未标识相关字段：%v；应包含以下任一线索 %q", err, clues)
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name            string
		size            int
		expanded, valid bool
	}{
		{"正文恰好1MiB", 1 << 20, false, true},
		{"正文超过1MiB", (1 << 20) + 1, false, false},
		{"环境替换后正文超过1MiB", (1 << 20) + 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := modelRequest()
			r.Body = strings.Repeat("x", tc.size)
			s := Settings{}
			if tc.expanded {
				s = Settings{Environment: "active", Environments: map[string]map[string]string{"active": {"payload": r.Body}}}
				r.Body = "{{payload}}"
			}
			got, err := Resolve(r, s)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Body) != tc.size {
					t.Fatal("大小边界改变了正文")
				}
				return
			}
			requireModelError(t, err, "大小", "超过", "MiB")
		})
	}
	// 文档：全局与单请求头合并、重复项保留、活动环境替换、不修改保存模板。
	t.Run("合并活动环境并保留重复头", func(t *testing.T) {
		r := modelRequest()
		r.URL, r.Body = " {{base}}/api/{{id}} ", `{"id":"{{id}}"}`
		r.Headers = []Pair{{"x-shared", "{{id}}"}, {"X-Shared", "second"}, {"X-Only", "request"}}
		s := Settings{Environment: "chosen", Environments: map[string]map[string]string{"chosen": {"base": "https://example.test", "id": "42"}, "other": {"id": "wrong"}}, GlobalHeaders: []Pair{{"X-Shared", "{{missing}}"}, {"X-Global", "{{id}}"}, {"X-Global", "second-global"}}}
		beforeR, beforeS := modelSnapshot(t, r), modelSnapshot(t, s)
		got, err := Resolve(r, s)
		if err != nil {
			t.Fatal(err)
		}
		want := modelRequest()
		want.URL, want.Body = "https://example.test/api/42", `{"id":"42"}`
		want.Headers = []Pair{{"X-Global", "42"}, {"X-Global", "second-global"}, {"x-shared", "42"}, {"X-Shared", "second"}, {"X-Only", "request"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("请求字段不符：got=%+v want=%+v", got, want)
		}
		if modelSnapshot(t, r) != beforeR || modelSnapshot(t, s) != beforeS {
			t.Fatal("解析修改了输入模板或环境")
		}
	})
	t.Run("输出请求头不共享模板内存", func(t *testing.T) {
		r := modelRequest()
		r.Headers = []Pair{{"X-Local", "before"}}
		s := Settings{GlobalHeaders: []Pair{{"X-Global", "before"}}}
		got, err := Resolve(r, s)
		if err != nil {
			t.Fatal(err)
		}
		for i := range got.Headers {
			got.Headers[i].Value = "changed"
		}
		if r.Headers[0].Value != "before" || s.GlobalHeaders[0].Value != "before" {
			t.Fatal("返回值与模板共用可变请求头")
		}
	})
	t.Run("去除名称空白后覆盖全局头", func(t *testing.T) {
		r := modelRequest()
		r.Headers = []Pair{{" x-mode ", "local"}}
		got, err := Resolve(r, Settings{GlobalHeaders: []Pair{{"X-Mode", "global"}}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Headers, []Pair{{"x-mode", "local"}}) {
			t.Fatalf("规范化后只能保留本地头：%+v", got.Headers)
		}
	})
	t.Run("仅使用活动环境", func(t *testing.T) {
		r := modelRequest()
		r.URL = "https://example.test/{{other_only}}"
		_, err := Resolve(r, Settings{Environment: "active", Environments: map[string]map[string]string{"active": {}, "inactive": {"other_only": "value"}}})
		requireModelError(t, err, "other_only")
	})
	for _, field := range []string{"地址", "正文", "全局请求头", "单请求头"} {
		t.Run("缺失变量/"+field, func(t *testing.T) {
			r, s := modelRequest(), Settings{}
			switch field {
			case "地址":
				r.URL = "https://example.test/{{missing}}"
			case "正文":
				r.Body = "{{missing}}"
			case "全局请求头":
				s.GlobalHeaders = []Pair{{"X-Value", "{{missing}}"}}
			case "单请求头":
				r.Headers = []Pair{{"X-Value", "{{missing}}"}}
			}
			beforeR, beforeS := modelSnapshot(t, r), modelSnapshot(t, s)
			_, err := Resolve(r, s)
			requireModelError(t, err, "missing")
			if modelSnapshot(t, r) != beforeR || modelSnapshot(t, s) != beforeS {
				t.Fatal("失败解析修改了输入")
			}
		})
	}
	for _, tc := range []struct {
		name, protocol, address string
		valid                   bool
	}{
		{"HTTP_HTTP", "HTTP", "http://example.test", true}, {"HTTP_HTTPS", "HTTP", "https://example.test", true},
		{"SSE_HTTP", "SSE", "http://example.test", true}, {"SSE_HTTPS", "SSE", "https://example.test", true},
		{"WebSocket_WS", "WebSocket", "ws://example.test", true}, {"WebSocket_WSS", "WebSocket", "wss://example.test", true},
		{"HTTP_WS", "HTTP", "ws://example.test", false}, {"SSE_WS", "SSE", "ws://example.test", false},
		{"WebSocket_HTTP", "WebSocket", "http://example.test", false}, {"未知协议", "gRPC", "https://example.test", false},
		{"空协议", "", "https://example.test", false}, {"FTP地址", "HTTP", "ftp://example.test", false},
	} {
		t.Run("协议/"+tc.name, func(t *testing.T) {
			r := modelRequest()
			r.Protocol, r.URL = tc.protocol, tc.address
			got, err := Resolve(r, Settings{})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if got.Protocol != tc.protocol || got.URL != tc.address {
					t.Fatalf("协议或地址被改变：%+v", got)
				}
				return
			}
			requireModelError(t, err, "协议", "http", "websocket", "scheme")
		})
	}
	for _, tc := range []struct{ name, address string }{{"无主机", "https:///path"}, {"相对路径", "/path"}, {"非法转义", "https://example.test/%zz"}, {"内嵌认证", "https://user:pass@example.test"}} {
		t.Run("非法地址/"+tc.name, func(t *testing.T) {
			r := modelRequest()
			r.URL = tc.address
			_, err := Resolve(r, Settings{})
			requireModelError(t, err, "地址", "url", "认证", "authorization")
		})
	}
	for _, tc := range []struct {
		name, method string
		valid        bool
	}{{"标准方法", "GET", true}, {"扩展方法", "CUSTOM-METHOD", true}, {"空方法", "", false}, {"含空格", "BAD METHOD", false}, {"含DEL", "GET\x7f", false}} {
		t.Run("方法/"+tc.name, func(t *testing.T) {
			r := modelRequest()
			r.Method = tc.method
			got, err := Resolve(r, Settings{})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if got.Method != tc.method {
					t.Fatal("自定义方法未保留")
				}
				return
			}
			requireModelError(t, err, "方法", "method")
		})
	}
	for _, tc := range []struct {
		name, key, value string
		valid            bool
	}{
		{"合法符号名称", "X-!#$%&'*+-.^_`|~", "yes", true}, {"制表符值", "X-Test", "a\tb", true}, {"非ASCII值", "X-Test", "中文", true},
		{"空名称", "", "v", false}, {"名称空格", "X Bad", "v", false}, {"名称DEL", "X-Bad\x7f", "v", false},
		{"值LF", "X-Test", "a\nb", false}, {"值CR", "X-Test", "a\rb", false}, {"值NUL", "X-Test", "a\x00b", false},
		{"值控制字符", "X-Test", "a\x01b", false}, {"值DEL", "X-Test", "a\x7fb", false}, {"替换后出现换行", "X-Test", "{{line}}", false},
	} {
		t.Run("请求头/"+tc.name, func(t *testing.T) {
			r := modelRequest()
			r.Headers = []Pair{{tc.key, tc.value}}
			got, err := Resolve(r, Settings{Environment: "active", Environments: map[string]map[string]string{"active": {"line": "a\nb"}}})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.Headers, r.Headers) {
					t.Fatalf("合法头被改变：%+v", got.Headers)
				}
				return
			}
			requireModelError(t, err, "请求头", "header")
		})
	}
	for _, tc := range []struct {
		name    string
		seconds int
		valid   bool
	}{{"正数", 1, true}, {"零", 0, false}, {"负数", -1, false}} {
		t.Run("超时/"+tc.name, func(t *testing.T) {
			r := modelRequest()
			r.TimeoutSeconds = tc.seconds
			got, err := Resolve(r, Settings{})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				if got.TimeoutSeconds != tc.seconds {
					t.Fatal("超时未保留")
				}
				return
			}
			requireModelError(t, err, "超时", "timeout")
		})
	}
}

func TestExpand(t *testing.T) {
	for _, tc := range []struct {
		name, input   string
		vars          map[string]string
		want, missing string
	}{
		{"空文本", "", nil, "", ""}, {"无占位文本", "原始 text", nil, "原始 text", ""},
		{"多个变量和重复引用", "{{a}}/{{b}}/{{a}}", map[string]string{"a": "one", "b": "two"}, "one/two/one", ""},
		{"空变量值", "left{{empty}}right", map[string]string{"empty": ""}, "leftright", ""},
		{"缺失变量", "{{missing}}", map[string]string{}, "", "missing"},
		{"部分变量缺失", "{{known}}/{{missing}}", map[string]string{"known": "yes"}, "", "missing"},
		{"空变量集合", "{{missing}}", nil, "", "missing"},
		{"变量名两侧空白", "{{ name }}", map[string]string{"name": "value"}, "value", ""},
		{"带点横线下划线的名称", "{{api.url}}/{{api-key}}/{{_name}}", map[string]string{"api.url": "host", "api-key": "key", "_name": "name"}, "host/key/name", ""},
		{"非递归替换", "{{first}}", map[string]string{"first": "{{second}}", "second": "expanded"}, "{{second}}", ""},
		{"系统变量保持字面量", "${QINGZHOU_TEST_VALUE} $QINGZHOU_TEST_VALUE %QINGZHOU_TEST_VALUE%", nil, "${QINGZHOU_TEST_VALUE} $QINGZHOU_TEST_VALUE %QINGZHOU_TEST_VALUE%", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := modelSnapshot(t, tc.vars)
			got, err := Expand(tc.input, tc.vars)
			if tc.missing != "" {
				requireModelError(t, err, tc.missing)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("got=%q want=%q", got, tc.want)
				}
			}
			if modelSnapshot(t, tc.vars) != before {
				t.Fatal("替换修改了环境表")
			}
		})
	}
}

func TestParseHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []Pair
		invalid     bool
	}{
		{"重复头和冒号值", "X-A: first\nX-A: second\nLocation: https://example.test:8443/a", []Pair{{"X-A", "first"}, {"X-A", "second"}, {"Location", "https://example.test:8443/a"}}, false},
		{"CRLF空行与空白", "\r\n  X-A : value  \r\n\t\r\nX-B: other\r\n", []Pair{{"X-A", "value"}, {"X-B", "other"}}, false},
		{"空文本", "", nil, false}, {"空值", "X-Empty:", []Pair{{"X-Empty", ""}}, false},
		{"合法符号与制表符值", "X-!#$%&'*+-.^_`|~: a\tb", []Pair{{"X-!#$%&'*+-.^_`|~", "a\tb"}}, false},
		{"缺少冒号", "X-A: good\ninvalid", nil, true}, {"空名称", ": value", nil, true}, {"名称空格", "X A: value", nil, true},
		{"名称DEL", "X-A\x7f: value", nil, true}, {"值NUL", "X-A: a\x00b", nil, true}, {"值CR", "X-A: a\rb", nil, true}, {"值控制字符", "X-A: a\x01b", nil, true}, {"值DEL", "X-A: a\x7fb", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseHeaders(tc.input)
			if tc.invalid {
				requireModelError(t, err, "请求头", "header")
				if len(got) != 0 {
					t.Fatalf("解析失败不应返回半成品：%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v want=%+v", got, tc.want)
			}
		})
	}
}

func TestFormatHeaders(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		if got := FormatHeaders(nil); got != "" {
			t.Fatalf("got=%q", got)
		}
	})
	t.Run("重复头与空值", func(t *testing.T) {
		h := []Pair{{"X-A", "one"}, {"X-A", "two:extra"}, {"X-Empty", ""}}
		before := modelSnapshot(t, h)
		if got := FormatHeaders(h); got != "X-A: one\nX-A: two:extra\nX-Empty: " {
			t.Fatalf("格式化丢失语义：%q", got)
		}
		if modelSnapshot(t, h) != before {
			t.Fatal("格式化修改了输入")
		}
	})
	t.Run("解析格式化往返", func(t *testing.T) {
		h := []Pair{{"X-A", "one"}, {"X-A", "two"}, {"X-Empty", ""}, {"Location", "https://example.test:8443/"}}
		got, err := ParseHeaders(FormatHeaders(h))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, h) {
			t.Fatalf("往返丢失语义：%+v", got)
		}
	})
}

func TestResolveMultipart(t *testing.T) {
	// A t.TempDir()-rooted path is absolute on every OS; the POSIX-rooted
	// "/tmp/..." literal is not absolute on Windows.
	filePath := filepath.Join(t.TempDir(), "{{literal}}.txt")
	r := modelRequest()
	r.BodyMode = BodyMultipart
	r.Body = "{{inactive}}"
	r.FormParts = []FormPart{
		{Kind: FormText, Name: "title", Value: "{{value}}"},
		{Kind: FormFile, Name: "files", FilePath: filePath},
	}
	s := NewWorkspace().Settings
	s.Environments[s.Environment]["value"] = "中文内容"
	s.GlobalHeaders = []Pair{{Key: "Content-Type", Value: "application/json"}, {Key: "X-Test", Value: "yes"}}
	r.Headers = []Pair{{Key: "content-type", Value: "multipart/form-data; boundary=wrong"}}
	before := modelSnapshot(t, r)
	got, err := Resolve(r, s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "" || got.FormParts[0].Value != "中文内容" || got.FormParts[1].FilePath != filePath {
		t.Fatalf("表单变量或正文处理错误：%+v", got)
	}
	if len(got.Headers) != 1 || got.Headers[0].Key != "X-Test" {
		t.Fatalf("Content-Type 应由 multipart 编码器生成：%+v", got.Headers)
	}
	got.FormParts[0].Value = "changed"
	if modelSnapshot(t, r) != before {
		t.Fatal("解析修改了保存的表单模板")
	}
	for name, mutate := range map[string]func(*Request){
		"未知正文类型":      func(r *Request) { r.BodyMode = "unknown" },
		"未知字段类型":      func(r *Request) { r.FormParts[0].Kind = "unknown" },
		"缺少字段名称":      func(r *Request) { r.FormParts[0].Name = "" },
		"字段名称含换行":     func(r *Request) { r.FormParts[0].Name = "bad\r\nname" },
		"未选择文件":       func(r *Request) { r.FormParts[1].FilePath = "" },
		"相对文件路径":      func(r *Request) { r.FormParts[1].FilePath = "relative.txt" },
		"过多字段":        func(r *Request) { r.FormParts = make([]FormPart, MaxFormParts+1) },
		"文本过大":        func(r *Request) { r.FormParts[0].Value = strings.Repeat("x", MaxInput+1) },
		"WebSocket表单": func(r *Request) { r.Protocol, r.URL = "WebSocket", "ws://example.test" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := r
			invalid.FormParts = append([]FormPart(nil), r.FormParts...)
			mutate(&invalid)
			if _, err := Resolve(invalid, s); err == nil {
				t.Fatal("无效表单未被拒绝")
			}
		})
	}
	t.Run("原始正文忽略未启用表单", func(t *testing.T) {
		r.BodyMode, r.Body = BodyRaw, "raw"
		r.FormParts = []FormPart{{Kind: FormFile}}
		got, err := Resolve(r, s)
		if err != nil || got.Body != "raw" {
			t.Fatalf("原始正文被未启用的表单干扰：%v", err)
		}
	})
}

func TestNewWorkspace(t *testing.T) {
	// 推断：首次打开即可编辑，有合法活动环境；不固定实现中的名称、版本号、超时常量。
	t.Run("初始草稿和活动环境", func(t *testing.T) {
		w := NewWorkspace()
		if w.Version <= 0 || w.Draft.ID == "" || w.Draft.Method == "" || w.Draft.Protocol == "" || w.Draft.TimeoutSeconds <= 0 {
			t.Fatalf("草稿或版本不可用：%+v", w)
		}
		if w.Draft.URL != "" {
			t.Fatalf("首次地址应留空等待用户输入：%q", w.Draft.URL)
		}
		if w.Settings.Environment == "" {
			t.Fatal("未选择活动环境")
		}
		if _, ok := w.Settings.Environments[w.Settings.Environment]; !ok {
			t.Fatal("活动环境不存在")
		}
	})
	t.Run("不同工作区状态独立", func(t *testing.T) {
		a, b := NewWorkspace(), NewWorkspace()
		if a.Draft.ID == b.Draft.ID {
			t.Fatal("独立草稿 ID 重复")
		}
		before := modelSnapshot(t, b)
		a.Settings.Environments[a.Settings.Environment]["only-in-a"] = "changed"
		a.Settings.Environments["new"] = map[string]string{"a": "b"}
		if modelSnapshot(t, b) != before {
			t.Fatal("独立工作区共享环境内存")
		}
	})
	t.Run("首次没有保存请求和历史", func(t *testing.T) {
		w := NewWorkspace()
		if len(w.Requests) != 0 || len(w.History) != 0 {
			t.Fatal("新建工作区携带请求或历史")
		}
	})
}
