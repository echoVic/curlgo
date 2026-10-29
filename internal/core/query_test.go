package core

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestQueryRoundTrip(t *testing.T) {
	for _, address := range []string{
		"https://example.test/path",
		"https://example.test/path?",
		"https://example.test/path?tag=one&tag=two&empty=&flag&=value&=#section",
		"https://example.test/path?sig=a%2fb%20c&literal=%7B%7Bx%7D%7D&&tail&",
		"{{base_url}}/path?中文=你好&space=a+b&query={{ value }}#anchor?not=query",
		"https://example.test/path#anchor?not=query",
	} {
		t.Run(address, func(t *testing.T) {
			params, err := ParseQueryParams(address)
			if err != nil {
				t.Fatal(err)
			}
			if got := WithQueryParams(address, params); got != address {
				t.Fatalf("未经编辑的地址被改写：%q", got)
			}
		})
	}
	params, err := ParseQueryParams("https://example.test/?tag=one&tag=two&empty=&flag&=value")
	if err != nil {
		t.Fatal(err)
	}
	var pairs []Pair
	for _, p := range params {
		pairs = append(pairs, Pair{p.Key, p.Value})
		if p.ID == "" {
			t.Fatal("参数缺少稳定标识")
		}
	}
	want := []Pair{{"tag", "one"}, {"tag", "two"}, {"empty", ""}, {"flag", ""}, {"", "value"}}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("重复键或空值丢失：%+v", pairs)
	}
	for _, address := range []string{"https://example.test/?bad=%zz", "https://example.test/?%xx=value"} {
		if _, err := ParseQueryParams(address); err == nil {
			t.Fatalf("无效编码未报告：%q", address)
		}
	}
}

func TestQueryEdits(t *testing.T) {
	address := "https://example.test/path?sig=a%2fb%20c&tag=one&flag#section"
	params, err := ParseQueryParams(address)
	if err != nil {
		t.Fatal(err)
	}
	params[1].Disabled = true
	params[2].Value = "你好 &+#="
	params = append(params, QueryParam{ID: "new", Key: "中文", Value: "{{ value }} /+"})
	got := WithQueryParams(address, params)
	want := "https://example.test/path?sig=a%2fb%20c&flag=%E4%BD%A0%E5%A5%BD+%26%2B%23%3D&%E4%B8%AD%E6%96%87={{ value }}+%2F%2B#section"
	if got != want {
		t.Fatalf("编辑后的地址错误：\ngot  %s\nwant %s", got, want)
	}
	params[1].Disabled = false
	if got := WithQueryParams(address, params); !strings.Contains(got, "&tag=one&") {
		t.Fatalf("重新启用的参数未恢复原位置：%q", got)
	}
	for i := range params {
		params[i].Disabled = true
	}
	if got := WithQueryParams(address, params); got != "https://example.test/path#section" {
		t.Fatalf("禁用全部参数未清除 Query：%q", got)
	}
	if got := WithQueryParams("https://example.test/", []QueryParam{{ID: "blank"}}); got != "https://example.test/" {
		t.Fatalf("新建空行不应修改地址：%q", got)
	}
}

func TestResolveQueryVariables(t *testing.T) {
	r := NewRequest()
	r.URL = "{{base_url}}/{{path}}?{{key}}={{value}}&q=pre{{value}}post&literal=%7B%7Bvalue%7D%7D&sig=a%2fb%20c#{{fragment}}"
	s := NewWorkspace().Settings
	s.Environments[s.Environment] = map[string]string{
		"base_url": "https://example.test", "path": "api", "fragment": "part",
		"key": "中文 &", "value": "你好 &+#=%2F",
	}
	before := modelSnapshot(t, r)
	got, err := Resolve(r, s)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/api" || u.Fragment != "part" || len(u.Query()) != 4 ||
		u.Query().Get("中文 &") != "你好 &+#=%2F" ||
		u.Query().Get("q") != "pre你好 &+#=%2Fpost" ||
		u.Query().Get("literal") != "{{value}}" ||
		!strings.Contains(u.RawQuery, "sig=a%2fb%20c") {
		t.Fatalf("变量改变了 Query 结构或原始编码：%q", got.URL)
	}
	if modelSnapshot(t, r) != before {
		t.Fatal("变量替换修改了保存的模板")
	}
	r.URL = "https://example.test/?q={{missing}}"
	if _, err := Resolve(r, s); err == nil {
		t.Fatal("未定义的 Query 变量未报告")
	}
}

func TestQueryEditsPreserveUntouchedComponents(t *testing.T) {
	address := "https://example.test/?key=%7b%7bvalue%7d%7d&sig%2fpart=a%20b&flag"
	params, err := ParseQueryParams(address)
	if err != nil {
		t.Fatal(err)
	}
	params[0].Key = "renamed"
	params[1].Value = "changed"
	params[2].Key = "renamedFlag"
	want := "https://example.test/?renamed=%7b%7bvalue%7d%7d&sig%2fpart=changed&renamedFlag"
	if got := WithQueryParams(address, params); got != want {
		t.Fatalf("编辑一个单元格改变了另一单元格的编码：\ngot  %s\nwant %s", got, want)
	}
	r := NewRequest()
	r.URL = WithQueryParams(address, params)
	if _, err := Resolve(r, NewWorkspace().Settings); err != nil {
		t.Fatalf("编码的字面量意外变成了变量：%v", err)
	}
}
