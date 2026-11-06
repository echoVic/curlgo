package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCURLMultipart(t *testing.T) {
	t.Run("文本字段安全往返", func(t *testing.T) {
		r := modelRequest()
		r.BodyMode, r.Body = BodyMultipart, "inactive"
		r.FormParts = []FormPart{
			{Kind: FormText, Name: "title", Value: "{{text}}"},
			{Kind: FormText, Name: "title", Value: "@literal;type=text/plain"},
		}
		s := NewWorkspace().Settings
		s.Environments[s.Environment]["text"] = "it's $(literal)\n中文"
		cmd, err := ExportCURL(r, s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ImportCURL(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if got.BodyMode != BodyMultipart || len(got.FormParts) != 2 || got.FormParts[0].Value != "it's $(literal)\n中文" || got.FormParts[1].Value != "@literal;type=text/plain" {
			t.Fatalf("curl 导出丢失表单语义：%s %+v", cmd, got)
		}
		if strings.Contains(cmd, "--data-raw") || strings.Contains(cmd, "Content-Type:") {
			t.Fatalf("multipart curl 使用了原始正文：%s", cmd)
		}
	})
	t.Run("文件路径在curl表单语法中转义", func(t *testing.T) {
		// Absolute on every OS; the double quote exercises curl form escaping
		// and the path never reaches the filesystem.
		filePath := filepath.Join(t.TempDir(), `a,b; "quote".txt`)
		r := modelRequest()
		r.BodyMode = BodyMultipart
		r.FormParts = []FormPart{{Kind: FormFile, Name: "file", FilePath: filePath}}
		cmd, err := ExportCURL(r, NewWorkspace().Settings)
		if err != nil {
			t.Fatal(err)
		}
		args, err := SplitShell(cmd)
		if err != nil {
			t.Fatal(err)
		}
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filePath)
		if args[len(args)-2] != "--form" || args[len(args)-1] != `file=@"`+escaped+`"` {
			t.Fatalf("文件路径转义错误：%q", args)
		}
		if _, err := ImportCURL(cmd); err == nil {
			t.Fatal("导入 curl 自动授权了本地文件读取")
		}
	})
	for _, cmd := range []string{
		`curl https://example.test --form-string 'missing-equals'`,
		`curl https://example.test --form-string 'name=value' -d 'raw'`,
		`curl https://example.test -d 'raw' --form-string 'name=value'`,
	} {
		t.Run(cmd, func(t *testing.T) {
			if _, err := ImportCURL(cmd); err == nil {
				t.Fatal("无效或混合正文没有拒绝")
			}
		})
	}
}

func TestSplitShell(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
		invalid     bool
	}{
		{"空文本", " \t\n", nil, false},
		{"单双引号及相邻片段", `curl "https://example.test/a b" 'a'"b"c ''`, []string{"curl", "https://example.test/a b", "abc", ""}, false},
		{"转义空格和续行", "curl a\\ b \\\nhttps://example.test", []string{"curl", "a b", "https://example.test"}, false},
		{"双引号保留普通反斜线", `curl "a\qb" "a\"b"`, []string{"curl", `a\qb`, `a"b`}, false},
		{"单引号字面特殊字符", "curl '$HOME `cmd` ; | & < > ( )'", []string{"curl", "$HOME `cmd` ; | & < > ( )"}, false},
		{"命令分隔符", "curl https://example.test; touch sentinel", nil, true},
		{"管道", "curl https://example.test | cat", nil, true},
		{"命令替换", "curl $(touch sentinel)", nil, true},
		{"反引号", "curl `touch sentinel`", nil, true},
		{"环境变量展开", "curl \"$QINGZHOU_TEST_VALUE\"", nil, true},
		{"未闭合引号", "curl 'abc", nil, true},
		{"尾部转义", "curl abc\\", nil, true},
		{"未引用NUL", "curl a\x00b", nil, true},
		{"单引号NUL", "curl 'a\x00b'", nil, true},
		{"双引号NUL", "curl \"a\x00b\"", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SplitShell(tc.input)
			if tc.invalid {
				requireModelError(t, err, "curl", "shell", "字符", "引号", "转义")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestImportCURL(t *testing.T) {
	for _, tc := range []struct {
		name, command, method, body string
		headers                     []Pair
		follow                      bool
		timeout                     int
	}{
		{"基础GET", `curl https://example.test/a`, "GET", "", nil, false, 0},
		{"短参数连接及重复头", `curl -XPATCH -HX-A:first -H 'X-A: second' --url=https://example.test/a -dfoo=1 --data=bar=2 -L -m 8`, "PATCH", "foo=1&bar=2", []Pair{{"X-A", "first"}, {"X-A", "second"}, {"Content-Type", "application/x-www-form-urlencoded"}}, true, 8},
		{"JSON隐式POST", `curl https://example.test/a --json '{"a":1}'`, "POST", `{"a":1}`, []Pair{{"Content-Type", "application/json"}, {"Accept", "application/json"}}, false, 0},
		{"显式JSON头优先", `curl https://example.test/a --json '{}' -H 'content-type: custom/json' -H 'accept: custom/type'`, "POST", `{}`, []Pair{{"content-type", "custom/json"}, {"accept", "custom/type"}}, false, 0},
		{"空正文仍POST", `curl https://example.test/a -d ''`, "POST", "", []Pair{{"Content-Type", "application/x-www-form-urlencoded"}}, false, 0},
		{"raw保留文件字面量", `curl https://example.test/a --data-raw '@not-a-file'`, "POST", "@not-a-file", []Pair{{"Content-Type", "application/x-www-form-urlencoded"}}, false, 0},
		{"binary保留换行", "curl https://example.test/a --data-binary 'a\r\nb'", "POST", "a\r\nb", []Pair{{"Content-Type", "application/x-www-form-urlencoded"}}, false, 0},
		// curl -d 仅在读取 @file 时剥离 CR/LF；直接参数中的换行必须保留。
		{"data字面换行保留", "curl https://example.test/a --data 'a\r\nb'", "POST", "a\r\nb", []Pair{{"Content-Type", "application/x-www-form-urlencoded"}}, false, 0},
		{"Basic与Cookie和客户端头", `curl https://example.test/a -u alice:pass -b 'a=1; b=2' -A 'Agent/1' -e https://ref.test`, "GET", "", []Pair{{"Authorization", "Basic YWxpY2U6cGFzcw=="}, {"Cookie", "a=1; b=2"}, {"User-Agent", "Agent/1"}, {"Referer", "https://ref.test"}}, false, 0},
		{"Bearer", `curl https://example.test/a --oauth2-bearer sample`, "GET", "", []Pair{{"Authorization", "Bearer sample"}}, false, 0},
		{"HEAD", `curl --head https://example.test/a`, "HEAD", "", nil, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ImportCURL(tc.command)
			if err != nil {
				t.Fatal(err)
			}
			if got.Method != tc.method || got.URL != "https://example.test/a" || got.Body != tc.body || got.FollowRedirects != tc.follow || !reflect.DeepEqual(got.Headers, tc.headers) || tc.timeout != 0 && got.TimeoutSeconds != tc.timeout {
				t.Fatalf("导入语义不符：got=%+v wantMethod=%q wantBody=%q wantHeaders=%+v", got, tc.method, tc.body, tc.headers)
			}
		})
	}
	for _, tc := range []struct{ name, command string }{
		{"非curl命令", `wget https://example.test`}, {"无地址", `curl -I`}, {"两个地址", `curl https://a.test https://b.test`},
		{"缺少参数值", `curl https://example.test -H`}, {"未知参数", `curl https://example.test --retry 2`},
		{"读取data文件", `curl https://example.test --data @/nonexistent`}, {"读取binary文件", `curl https://example.test --data-binary @/nonexistent`},
		{"读取header文件", `curl https://example.test -H @/nonexistent`}, {"读取cookie文件", `curl https://example.test -b /nonexistent`},
		{"非法超时", `curl https://example.test -m NaN`}, {"分数超时", `curl https://example.test -m 1.5`},
		{"布尔参数附值", `curl https://example.test --location=false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ImportCURL(tc.command)
			if err == nil {
				t.Fatalf("应拒绝有损或不支持的命令：%s", tc.command)
			}
		})
	}
	t.Run("不执行shell", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "marker")
		_, err := ImportCURL("curl https://example.test; touch '" + marker + "'")
		if err == nil {
			t.Fatal("应拒绝shell命令链")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("解析执行了shell副作用")
		}
	})
	t.Run("内嵌认证转请求头", func(t *testing.T) {
		r, err := ImportCURL(`curl https://alice:pass@example.test/a`)
		if err != nil {
			t.Fatal(err)
		}
		if r.URL != "https://example.test/a" || !reflect.DeepEqual(r.Headers, []Pair{{"Authorization", "Basic YWxpY2U6cGFzcw=="}}) {
			t.Fatalf("认证语义丢失：%+v", r)
		}
	})
}

func TestExportCURL(t *testing.T) {
	t.Run("SSE默认Accept语义", func(t *testing.T) {
		r := modelRequest()
		r.Protocol = "SSE"
		r.Body = ""
		command, err := ExportCURL(r, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		imported, err := ImportCURL(command)
		if err != nil {
			t.Fatal(err)
		}
		if HTTPHeaders(imported.Headers).Get("Accept") != "text/event-stream" {
			t.Fatal("SSE导出丢失默认Accept")
		}
	})
	t.Run("HEAD包含头请求选项", func(t *testing.T) {
		r := modelRequest()
		r.Method = "HEAD"
		r.Body = ""
		command, err := ExportCURL(r, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		args, err := SplitShell(command)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, arg := range args {
			found = found || arg == "--head"
		}
		if !found {
			t.Fatal("curl使用HEAD方法时还需--head以免等待响应体")
		}
	})
	t.Run("模板重复头及特殊字符往返", func(t *testing.T) {
		r := modelRequest()
		r.URL = "https://example.test/{{id}}?x=1&y=2"
		r.Body = "line 1\nO'Reilly $literal `literal`"
		r.Headers = []Pair{{"X-Dup", "one"}, {"X-Dup", "two"}, {"Content-Type", "text/plain"}}
		s := Settings{Environment: "active", Environments: map[string]map[string]string{"active": {"id": "42"}}, GlobalHeaders: []Pair{{"X-Global", "yes"}}}
		beforeR, beforeS := modelSnapshot(t, r), modelSnapshot(t, s)
		command, err := ExportCURL(r, s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ImportCURL(command)
		if err != nil {
			t.Fatal(err)
		}
		if got.URL != "https://example.test/42?x=1&y=2" || got.Method != r.Method || got.Body != r.Body || got.TimeoutSeconds != r.TimeoutSeconds || got.FollowRedirects != r.FollowRedirects || !reflect.DeepEqual(got.Headers, []Pair{{"X-Global", "yes"}, {"X-Dup", "one"}, {"X-Dup", "two"}, {"Content-Type", "text/plain"}}) {
			t.Fatalf("导出再导入改变请求：%+v", got)
		}
		if beforeR != modelSnapshot(t, r) || beforeS != modelSnapshot(t, s) {
			t.Fatal("导出修改了模板")
		}
	})
	t.Run("默认正文类型保持网络语义", func(t *testing.T) {
		r := modelRequest()
		r.Headers = nil
		command, err := ExportCURL(r, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := ImportCURL(command)
		if err != nil {
			t.Fatal(err)
		}
		if h := HTTPHeaders(got.Headers).Get("Content-Type"); h != "application/octet-stream" {
			t.Fatalf("Engine.Run 默认正文类型为 application/octet-stream，导出后变成 %q", h)
		}
	})
	t.Run("SSE禁缓冲", func(t *testing.T) {
		r := modelRequest()
		r.Protocol = "SSE"
		r.Body = ""
		command, err := ExportCURL(r, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		args, err := SplitShell(command)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, a := range args {
			found = found || a == "--no-buffer"
		}
		if !found {
			t.Fatal("SSE导出遗漏禁缓冲")
		}
	})
	t.Run("拒绝WebSocket", func(t *testing.T) {
		r := modelRequest()
		r.Protocol = "WebSocket"
		r.URL = "ws://example.test"
		_, err := ExportCURL(r, Settings{})
		requireModelError(t, err, "HTTP", "SSE", "WebSocket")
	})
	t.Run("缺失环境变量不导出", func(t *testing.T) {
		r := modelRequest()
		r.Body = "{{missing}}"
		command, err := ExportCURL(r, Settings{})
		requireModelError(t, err, "missing")
		if strings.TrimSpace(command) != "" {
			t.Fatal("缺失变量时不应给出可执行命令")
		}
	})
}
