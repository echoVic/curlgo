package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type CollectionFormat string

const (
	CollectionCurlGo  CollectionFormat = "curlgo"
	CollectionPostman CollectionFormat = "postman"
)

type CollectionExportOptions struct {
	Name               string
	GroupID            string
	Format             CollectionFormat
	IncludeEnvironment bool
}

type CollectionExportResult struct {
	Data         []byte
	RequestCount int
	Warnings     []string
}

// ExportCollection produces an independent snapshot without expanding templates,
// reading multipart files, or including workspace runtime state.
func ExportCollection(w Workspace, opts CollectionExportOptions) (CollectionExportResult, error) {
	if opts.Format != CollectionCurlGo && opts.Format != CollectionPostman {
		return CollectionExportResult{}, fmt.Errorf("不支持的集合格式 %q", opts.Format)
	}
	if err := ValidateRequestGroups(w.Groups); err != nil {
		return CollectionExportResult{}, err
	}
	var selected map[string]bool
	if opts.GroupID != "" {
		selected = GroupSubtree(w.Groups, opts.GroupID)
		if len(selected) == 0 {
			return CollectionExportResult{}, fmt.Errorf("所选分组不存在")
		}
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "CurlGo"
		for _, g := range w.Groups {
			if g.ID == opts.GroupID {
				name = g.Name
				break
			}
		}
	}
	document := nativeCollection{
		Format: "curlgo-collection", Version: 1, Name: name,
		Groups: []RequestGroup{}, Requests: []Request{}, RootGroupID: opts.GroupID,
	}
	var warnings CollectionImport
	knownGroups := make(map[string]bool, len(w.Groups))
	for _, g := range w.Groups {
		knownGroups[g.ID] = true
		if selected != nil && !selected[g.ID] {
			continue
		}
		if g.ID == opts.GroupID {
			g.ParentID = ""
		}
		document.Groups = append(document.Groups, g)
	}
	for _, r := range w.Requests {
		if selected != nil && !selected[r.GroupID] {
			continue
		}
		r.Headers = slices.Clone(r.Headers)
		r.FormParts = slices.Clone(r.FormParts)
		r.QueryParams = slices.Clone(r.QueryParams)
		r.Extractors = slices.Clone(r.Extractors)
		r.Assertions = slices.Clone(r.Assertions)
		if r.GroupID != "" && !knownGroups[r.GroupID] {
			warnings.warn("「%s」引用了不存在的分组 %q，已按未分组导出，原收藏不变", r.Name, r.GroupID)
			r.GroupID = ""
		}
		if opts.IncludeEnvironment {
			r.Headers = mergeCollectionHeaders(w.Settings.GlobalHeaders, r.Headers)
		}
		document.Requests = append(document.Requests, r)
	}
	if len(document.Requests) == 0 {
		return CollectionExportResult{}, fmt.Errorf("所选范围没有请求，未生成导出文件")
	}
	if opts.IncludeEnvironment {
		document.Environments = make(map[string]map[string]string, len(w.Settings.Environments))
		omittedSecrets := false
		for env, vars := range w.Settings.Environments {
			if vars == nil {
				document.Environments[env] = nil
				continue
			}
			copyVars := make(map[string]string, len(vars))
			for key, value := range vars {
				if strings.HasPrefix(key, SecretPrefix) {
					omittedSecrets = true
					continue
				}
				copyVars[key] = value
			}
			document.Environments[env] = copyVars
		}
		if omittedSecrets {
			warnings.warn("已省略 secret.* 保留命名空间的环境变量值；机密变量不会随集合导出")
		}
		document.Environment = w.Settings.Environment
	}
	result := CollectionExportResult{}
	if opts.Format == CollectionPostman {
		requests := document.Requests[:0]
		for _, r := range document.Requests {
			if r.Protocol != "HTTP" {
				warnings.warn("已跳过「%s」：Postman 导出仅支持 HTTP，不支持 %s", collectionRequestPath(document.Groups, r), r.Protocol)
				continue
			}
			requests = append(requests, r)
		}
		document.Requests = requests
		if len(requests) == 0 {
			return CollectionExportResult{}, fmt.Errorf("所选范围没有可导出的 HTTP 请求，未生成 Postman 文件")
		}
	}
	result.Warnings = warnings.Warnings
	if err := validateNativeCollection(document); err != nil {
		return CollectionExportResult{}, err
	}
	var payload any = document
	if opts.Format == CollectionPostman {
		var warnings []string
		payload, warnings = encodePostmanCollection(document)
		result.Warnings = append(result.Warnings, warnings...)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return CollectionExportResult{}, err
	}
	if len(data) > maxWorkspace {
		return CollectionExportResult{}, fmt.Errorf("集合超过 16 MiB")
	}
	result.Data, result.RequestCount = data, len(document.Requests)
	return result, nil
}

// SaveCollection uses the workspace's durable atomic write and 0600 permissions.
func SaveCollection(path string, data []byte) error {
	return atomicWrite(path, data)
}

func mergeCollectionHeaders(globals, request []Pair) []Pair {
	override := make(map[string]bool, len(request))
	for _, h := range request {
		override[strings.ToLower(strings.TrimSpace(h.Key))] = true
	}
	headers := make([]Pair, 0, len(globals)+len(request))
	for _, h := range globals {
		if !override[strings.ToLower(strings.TrimSpace(h.Key))] {
			headers = append(headers, h)
		}
	}
	return append(headers, request...)
}

func collectionRequestPath(groups []RequestGroup, r Request) string {
	if r.GroupID == "" {
		return r.Name
	}
	return GroupPath(groups, r.GroupID) + " / " + r.Name
}

const collectionPostmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

type exportPostmanCollection struct {
	Info struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item     []exportPostmanItem     `json:"item"`
	Variable []exportPostmanVariable `json:"variable,omitempty"`
}

type exportPostmanItem struct {
	Name     string                 `json:"name"`
	Item     *[]exportPostmanItem   `json:"item,omitempty"`
	Request  *exportPostmanRequest  `json:"request,omitempty"`
	Behavior *exportPostmanBehavior `json:"protocolProfileBehavior,omitempty"`
}

type exportPostmanBehavior struct {
	FollowRedirects bool `json:"followRedirects"`
}

type exportPostmanRequest struct {
	Method string             `json:"method"`
	URL    string             `json:"url"`
	Header []Pair             `json:"header,omitempty"`
	Auth   *exportPostmanAuth `json:"auth,omitempty"`
	Body   exportPostmanBody  `json:"body"`
}

type exportPostmanAuth struct {
	Type   string                  `json:"type"`
	Bearer []exportPostmanVariable `json:"bearer,omitempty"`
	Basic  []exportPostmanVariable `json:"basic,omitempty"`
	APIKey []exportPostmanVariable `json:"apikey,omitempty"`
}

type exportPostmanBody struct {
	Mode     string                    `json:"mode"`
	Raw      string                    `json:"raw,omitempty"`
	FormData *[]exportPostmanFormPart  `json:"formdata,omitempty"`
	Options  *exportPostmanBodyOptions `json:"options,omitempty"`
}

type exportPostmanBodyOptions struct {
	Raw struct {
		Language string `json:"language"`
	} `json:"raw"`
}

type exportPostmanFormPart struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	Src   string `json:"src,omitempty"`
}

type exportPostmanVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func encodePostmanCollection(document nativeCollection) (exportPostmanCollection, []string) {
	out := exportPostmanCollection{}
	out.Info.Name, out.Info.Schema = document.Name, collectionPostmanSchema
	children := make(map[string][]RequestGroup)
	requests := make(map[string][]Request)
	for _, g := range document.Groups {
		children[g.ParentID] = append(children[g.ParentID], g)
	}
	var warnings CollectionImport
	var extractionWarnings []string
	var assertionWarnings []string
	for _, r := range document.Requests {
		requests[r.GroupID] = append(requests[r.GroupID], r)
		path := collectionRequestPath(document.Groups, r)
		if r.TimeoutSeconds != 30 {
			warnings.warn("「%s」的自定义超时为 %d 秒，Postman 格式不会保留此设置", path, r.TimeoutSeconds)
		}
		if len(r.QueryParams) > 0 {
			warnings.warn("「%s」的 Query 编辑信息（包括禁用行）不会保留，原始 URL 不变", path)
		}
		if len(r.Extractors) > 0 {
			extractionWarnings = append(extractionWarnings, fmt.Sprintf("「%s」的响应提取规则（包括禁用行）不会在 Postman 格式中保留；CurlGo 格式可完整保留提取配置", path))
		}
		if len(r.Assertions) > 0 {
			assertionWarnings = append(assertionWarnings, fmt.Sprintf("「%s」的响应断言规则（包括禁用行）不会在 Postman 格式中保留；CurlGo 格式可完整保留断言配置", path))
		}
		if r.BodyMode == BodyRaw && r.Body != "" {
			hasContentType := false
			for _, h := range r.Headers {
				hasContentType = hasContentType || strings.EqualFold(strings.TrimSpace(h.Key), "Content-Type")
			}
			if !hasContentType {
				warnings.warn("「%s」的原始正文未指定 Content-Type，Postman 可能自动添加 text/plain；CurlGo 格式可完整保留请求设置", path)
			}
		}
	}
	var items func(parent string) []exportPostmanItem
	items = func(parent string) []exportPostmanItem {
		list := []exportPostmanItem{}
		for _, g := range children[parent] {
			nested := items(g.ID)
			list = append(list, exportPostmanItem{Name: g.Name, Item: &nested})
		}
		for _, r := range requests[parent] {
			list = append(list, exportPostmanItem{
				Name: r.Name, Request: collectionPostmanRequest(r),
				Behavior: &exportPostmanBehavior{FollowRedirects: r.FollowRedirects},
			})
		}
		return list
	}
	out.Item = items("")
	vars := document.Environments[document.Environment]
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		out.Variable = append(out.Variable, exportPostmanVariable{Key: key, Value: vars[key], Type: "string"})
	}
	return out, append(append(warnings.Warnings, extractionWarnings...), assertionWarnings...)
}

func collectionPostmanRequest(r Request) *exportPostmanRequest {
	out := &exportPostmanRequest{Method: r.Method, URL: r.URL, Header: r.Headers}
	switch r.Auth.Mode {
	case AuthBearer:
		out.Auth = &exportPostmanAuth{Type: "bearer", Bearer: []exportPostmanVariable{
			{Key: "token", Value: r.Auth.Token, Type: "string"},
		}}
	case AuthBasic:
		out.Auth = &exportPostmanAuth{Type: "basic", Basic: []exportPostmanVariable{
			{Key: "username", Value: r.Auth.Username, Type: "string"},
			{Key: "password", Value: r.Auth.Password, Type: "string"},
		}}
	case AuthAPIKey:
		location := r.Auth.Location
		if location == "" {
			location = APIKeyHeader
		}
		out.Auth = &exportPostmanAuth{Type: "apikey", APIKey: []exportPostmanVariable{
			{Key: "key", Value: r.Auth.Key, Type: "string"},
			{Key: "value", Value: r.Auth.Value, Type: "string"},
			{Key: "in", Value: string(location), Type: "string"},
		}}
	}
	if r.BodyMode == BodyMultipart {
		out.Header = make([]Pair, 0, len(r.Headers))
		for _, h := range r.Headers {
			if !strings.EqualFold(strings.TrimSpace(h.Key), "Content-Type") {
				out.Header = append(out.Header, h)
			}
		}
		parts := []exportPostmanFormPart{}
		for _, p := range r.FormParts {
			part := exportPostmanFormPart{Key: p.Name, Type: string(p.Kind)}
			if p.Kind == FormFile {
				part.Src = p.FilePath
			} else {
				part.Value = p.Value
			}
			parts = append(parts, part)
		}
		out.Body = exportPostmanBody{Mode: "formdata", FormData: &parts}
		return out
	}
	options := &exportPostmanBodyOptions{}
	// Postman's schema leaves raw options open. "plain" avoids implying a
	// Content-Type when CurlGo reimports a headerless raw body.
	options.Raw.Language = "plain"
	out.Body = exportPostmanBody{Mode: "raw", Raw: r.Body, Options: options}
	return out
}
