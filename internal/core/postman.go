package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type postmanKV struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Disabled    bool            `json:"disabled"`
	Type        string          `json:"type"`
	Src         json.RawMessage `json:"src"`
	ContentType string          `json:"contentType"`
	In          string          `json:"in"`
}

func (kv postmanKV) text() string { return jsonText(kv.Value) }

// jsonText reads Postman's loosely typed values: strings stay as-is and
// numbers or booleans keep their JSON spelling.
func jsonText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var v any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return ""
	}
	if _, nested := v.(map[string]any); nested {
		return ""
	}
	if _, nested := v.([]any); nested {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

type postmanAuth struct {
	Type   string
	Fields map[string]string
}

func (a *postmanAuth) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw["type"], &a.Type); err != nil {
		return fmt.Errorf("认证类型无效")
	}
	a.Fields = map[string]string{}
	var attrs []postmanKV
	if json.Unmarshal(raw[a.Type], &attrs) == nil {
		for _, kv := range attrs {
			a.Fields[kv.Key] = kv.text()
		}
	}
	return nil
}

type postmanEvent struct {
	Listen   string `json:"listen"`
	Disabled bool   `json:"disabled"`
	Script   struct {
		Exec json.RawMessage `json:"exec"`
	} `json:"script"`
}

type postmanItem struct {
	Name     string          `json:"name"`
	Item     []postmanItem   `json:"item"`
	Request  json.RawMessage `json:"request"`
	Auth     *postmanAuth    `json:"auth"`
	Event    []postmanEvent  `json:"event"`
	Variable []postmanKV     `json:"variable"`
	Behavior struct {
		FollowRedirects *bool `json:"followRedirects"`
	} `json:"protocolProfileBehavior"`
}

type postmanBody struct {
	Mode       string          `json:"mode"`
	Disabled   bool            `json:"disabled"`
	Raw        string          `json:"raw"`
	URLEncoded []postmanKV     `json:"urlencoded"`
	FormData   []postmanKV     `json:"formdata"`
	GraphQL    json.RawMessage `json:"graphql"`
	Options    struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options"`
}

type postmanRequest struct {
	Method string          `json:"method"`
	URL    json.RawMessage `json:"url"`
	Header json.RawMessage `json:"header"`
	Auth   *postmanAuth    `json:"auth"`
	Body   *postmanBody    `json:"body"`
}

func hasScript(events []postmanEvent) bool {
	for _, e := range events {
		var lines []string
		if json.Unmarshal(e.Script.Exec, &lines) != nil {
			var line string
			if json.Unmarshal(e.Script.Exec, &line) == nil {
				lines = []string{line}
			}
		}
		if !e.Disabled && strings.TrimSpace(strings.Join(lines, "")) != "" {
			return true
		}
	}
	return false
}

// ImportPostman reads request templates and folder hierarchy from v2.1.
// Collection variables become an environment; scripts are never executed.
func ImportPostman(data []byte) (CollectionImport, error) {
	out := CollectionImport{Environments: map[string]map[string]string{}}
	var document struct {
		Info struct {
			Name   string `json:"name"`
			Schema string `json:"schema"`
		} `json:"info"`
		Item     []postmanItem  `json:"item"`
		Auth     *postmanAuth   `json:"auth"`
		Event    []postmanEvent `json:"event"`
		Variable []postmanKV    `json:"variable"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return out, fmt.Errorf("不是有效的 Postman 集合 JSON：%w", err)
	}
	if !strings.Contains(document.Info.Schema, "/collection/v2.1") {
		return out, fmt.Errorf("仅支持 Postman Collection v2.1 格式，请在 Postman 中导出为 v2.1")
	}
	out.Name = strings.TrimSpace(document.Info.Name)
	if out.Name == "" {
		out.Name = "Postman"
	}
	out.RootGroupID = NewID()
	out.Groups = append(out.Groups, RequestGroup{ID: out.RootGroupID, Name: out.Name})
	vars := map[string]string{}
	for _, v := range document.Variable {
		if !v.Disabled && v.Key != "" {
			vars[v.Key] = v.text()
		}
	}
	if len(vars) > 0 {
		out.Environments[out.Name] = vars
	}
	if hasScript(document.Event) {
		out.warn("集合「%s」包含脚本，CurlGo 不会执行脚本，依赖脚本的变量或签名需要手动设置", out.Name)
	}
	var walk func(items []postmanItem, prefix, groupID string, auth *postmanAuth, depth int)
	walk = func(items []postmanItem, prefix, groupID string, auth *postmanAuth, depth int) {
		for _, item := range items {
			leaf := strings.TrimSpace(item.Name)
			if leaf == "" {
				leaf = "未命名"
			}
			name := leaf
			if prefix != "" {
				name = prefix + " / " + name
			}
			if item.Request == nil {
				if item.Item == nil {
					out.warn("「%s」没有请求内容，已跳过", name)
					continue
				}
				if depth >= MaxGroupDepth-1 || len(out.Groups) >= MaxGroups {
					out.warn("「%s」文件夹层级或数量超过限制，已跳过", name)
					continue
				}
				folderAuth := auth
				if item.Auth != nil && item.Auth.Type != "inherit" {
					folderAuth = item.Auth
				}
				if hasScript(item.Event) {
					out.warn("文件夹「%s」包含脚本，CurlGo 不会执行", name)
				}
				if len(item.Variable) > 0 {
					out.warn("文件夹「%s」定义了变量，未导入，请在环境中设置", name)
				}
				child := NewID()
				out.Groups = append(out.Groups, RequestGroup{ID: child, Name: leaf, ParentID: groupID})
				walk(item.Item, name, child, folderAuth, depth+1)
				continue
			}
			if len(out.Requests) >= MaxImportRequests {
				out.warn("请求超过 %d 个，其余请求未导入", MaxImportRequests)
				return
			}
			r, err := postmanToRequest(&out, item, name, auth)
			if err != nil {
				out.warn("已跳过「%s」：%v", name, err)
				continue
			}
			if hasScript(item.Event) {
				out.warn("「%s」包含脚本，CurlGo 不会执行", name)
			}
			r.Name = leaf
			r.GroupID = groupID
			out.Requests = append(out.Requests, r)
		}
	}
	walk(document.Item, "", out.RootGroupID, document.Auth, 0)
	return out, ValidateRequestGroups(out.Groups)
}

func postmanToRequest(out *CollectionImport, item postmanItem, name string, inherited *postmanAuth) (Request, error) {
	r := NewRequest()
	r.Name = name
	if r.Name == "" {
		r.Name = "未命名请求"
	}
	var source postmanRequest
	var address string
	if json.Unmarshal(item.Request, &address) == nil {
		source.Method = "GET"
		source.URL, _ = json.Marshal(address)
	} else if err := json.Unmarshal(item.Request, &source); err != nil {
		return r, fmt.Errorf("请求格式无效")
	}
	if item.Behavior.FollowRedirects != nil {
		r.FollowRedirects = *item.Behavior.FollowRedirects
	}
	r.Method = strings.ToUpper(strings.TrimSpace(source.Method))
	if r.Method == "" {
		r.Method = "GET"
	}
	if json.Unmarshal(source.URL, &r.URL) != nil {
		var u struct {
			Raw      string      `json:"raw"`
			Variable []postmanKV `json:"variable"`
		}
		if err := json.Unmarshal(source.URL, &u); err != nil {
			return r, fmt.Errorf("请求地址格式无效")
		}
		path := map[string]string{}
		for _, v := range u.Variable {
			if !v.Disabled {
				path[v.Key] = v.text()
			}
		}
		r.URL = substitutePathParams(u.Raw, path)
	}
	r.URL = strings.TrimSpace(r.URL)
	var headers []postmanKV
	if len(source.Header) > 0 && string(source.Header) != "null" && json.Unmarshal(source.Header, &headers) != nil {
		var text string
		if json.Unmarshal(source.Header, &text) != nil {
			return r, fmt.Errorf("请求头格式无效")
		}
		parsed, err := ParseHeaders(text)
		if err != nil {
			return r, err
		}
		r.Headers = parsed
	}
	for _, h := range headers {
		if !h.Disabled {
			r.Headers = append(r.Headers, Pair{strings.TrimSpace(h.Key), h.text()})
		}
	}
	if b := source.Body; b != nil && !b.Disabled {
		if err := applyPostmanBody(out, &r, name, b); err != nil {
			return r, err
		}
	}
	auth := inherited
	if source.Auth != nil && source.Auth.Type != "inherit" {
		auth = source.Auth
	}
	if auth != nil {
		if err := applyPostmanAuth(&r, *auth); err != nil {
			return r, err
		}
	}
	return r, checkImported(r)
}

func applyPostmanBody(out *CollectionImport, r *Request, name string, b *postmanBody) error {
	switch b.Mode {
	case "", "none":
	case "raw":
		r.Body = b.Raw
		language := b.Options.Raw.Language
		if language == "" {
			language = "text"
		}
		if r.Body != "" {
			r.Headers = withDefaultContentType(r.Headers, defaultContentTypes[language])
		}
	case "urlencoded":
		var fields []formField
		for _, kv := range b.URLEncoded {
			if !kv.Disabled {
				fields = append(fields, formField{kv.Key, kv.text()})
			}
		}
		body, hasVariable := encodeForm(fields)
		if hasVariable {
			out.warn("「%s」的表单包含变量，发送时变量值不会再次编码，值含 & = + 等字符时请手动编码", name)
		}
		r.Body = body
		r.Headers = withDefaultContentType(r.Headers, defaultContentTypes["form"])
	case "formdata":
		r.BodyMode = BodyMultipart
		for _, kv := range b.FormData {
			if kv.Disabled {
				continue
			}
			if kv.ContentType != "" {
				return fmt.Errorf("字段 %s 指定了 Content-Type，暂不支持", kv.Key)
			}
			if kv.Type == "file" {
				path, ok := singleFile(kv.Src)
				if !ok {
					return fmt.Errorf("文件字段 %s 未选择单个文件", kv.Key)
				}
				if !filepath.IsAbs(path) {
					return fmt.Errorf("文件字段 %s 使用相对路径 %s，无法确定位置", kv.Key, path)
				}
				out.warn("「%s」引用本地文件 %s，发送时才会读取，请确认文件存在", name, path)
				r.FormParts = append(r.FormParts, FormPart{ID: NewID(), Kind: FormFile, Name: kv.Key, FilePath: path})
			} else {
				r.FormParts = append(r.FormParts, FormPart{ID: NewID(), Kind: FormText, Name: kv.Key, Value: kv.text()})
			}
		}
		var headers []Pair
		for _, h := range r.Headers {
			if !strings.EqualFold(h.Key, "Content-Type") {
				headers = append(headers, h)
			}
		}
		r.Headers = headers
	case "graphql":
		var g struct {
			Query     string `json:"query"`
			Variables string `json:"variables"`
		}
		if err := json.Unmarshal(b.GraphQL, &g); err != nil {
			return fmt.Errorf("GraphQL 正文格式无效")
		}
		body, err := graphQLBody(g.Query, g.Variables)
		if err != nil {
			return err
		}
		r.Body = body
		r.Headers = withDefaultContentType(r.Headers, defaultContentTypes["json"])
	case "file":
		return fmt.Errorf("二进制文件正文暂不支持")
	default:
		return fmt.Errorf("不支持的正文类型 %s", b.Mode)
	}
	return nil
}

func applyPostmanAuth(r *Request, a postmanAuth) error {
	switch a.Type {
	case "noauth":
		r.Auth = Authentication{}
	case "bearer":
		r.Auth = Authentication{Mode: AuthBearer, Token: a.Fields["token"]}
	case "basic":
		r.Auth = Authentication{Mode: AuthBasic, Username: a.Fields["username"], Password: a.Fields["password"]}
	case "apikey":
		location := APIKeyLocation(a.Fields["in"])
		if location == "" {
			location = APIKeyHeader
		}
		r.Auth = Authentication{Mode: AuthAPIKey, Key: a.Fields["key"], Value: a.Fields["value"], Location: location}
	default:
		return fmt.Errorf("暂不支持 %s 认证", a.Type)
	}
	return nil
}

// singleFile reads a form file source, which Postman writes as a string or,
// for files picked in a list, an array.
func singleFile(src json.RawMessage) (string, bool) {
	var path string
	if json.Unmarshal(src, &path) != nil {
		var paths []string
		if json.Unmarshal(src, &paths) != nil || len(paths) != 1 {
			return "", false
		}
		path = paths[0]
	}
	return path, path != ""
}
