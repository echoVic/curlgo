package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxNativeCollectionRequests = 5000

type nativeCollection struct {
	Format       string                       `json:"format"`
	Version      int                          `json:"version"`
	Name         string                       `json:"name"`
	Groups       []RequestGroup               `json:"groups"`
	Requests     []Request                    `json:"requests"`
	RootGroupID  string                       `json:"rootGroupId,omitempty"`
	Environments map[string]map[string]string `json:"environments,omitempty"`
	Environment  string                       `json:"environment,omitempty"`
}

// ImportCurlGoCollection reads templates only. IDs are preserved for the caller
// to remap when the user confirms the import.
func ImportCurlGoCollection(data []byte) (CollectionImport, error) {
	if len(data) > maxWorkspace {
		return CollectionImport{}, fmt.Errorf("集合超过 16 MiB")
	}
	if !utf8.Valid(data) {
		return CollectionImport{}, fmt.Errorf("集合必须使用有效的 UTF-8 编码")
	}
	var document nativeCollection
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return CollectionImport{}, fmt.Errorf("不是有效的 CurlGo 集合 JSON：%w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return CollectionImport{}, fmt.Errorf("集合只能包含一个 JSON 文档")
	}
	if document.Format != "curlgo-collection" {
		return CollectionImport{}, fmt.Errorf("仅支持 format 为 curlgo-collection 的集合")
	}
	if document.Version != 1 {
		return CollectionImport{}, fmt.Errorf("不支持的 CurlGo 集合版本 %d", document.Version)
	}
	if err := validateNativeCollection(document); err != nil {
		return CollectionImport{}, err
	}
	out := CollectionImport{
		Name: document.Name, Requests: document.Requests, Groups: document.Groups,
		RootGroupID: document.RootGroupID, Environments: document.Environments,
		Environment: document.Environment,
	}
	for _, r := range out.Requests {
		for _, part := range r.FormParts {
			if part.Kind == FormFile && part.FilePath != "" {
				out.warn("「%s」引用本地文件 %s，发送时才会读取，请确认文件存在", collectionRequestPath(out.Groups, r), part.FilePath)
			}
		}
	}
	return out, nil
}

func validCollectionMetadata(text string, limit int) bool {
	if strings.TrimSpace(text) == "" || len(text) > limit || !utf8.ValidString(text) {
		return false
	}
	for _, c := range text {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}

func validateNativeCollection(document nativeCollection) error {
	if !validCollectionMetadata(document.Name, 1024) {
		return fmt.Errorf("集合名称无效，不能为空、包含控制字符或超过 1024 字节")
	}
	if document.Requests == nil || document.Groups == nil {
		return fmt.Errorf("集合的 requests 和 groups 必须为数组")
	}
	if len(document.Requests) > maxNativeCollectionRequests {
		return fmt.Errorf("集合最多支持 %d 个请求", maxNativeCollectionRequests)
	}
	if err := ValidateRequestGroups(document.Groups); err != nil {
		return err
	}
	groups := make(map[string]RequestGroup, len(document.Groups))
	for _, g := range document.Groups {
		if !validCollectionMetadata(g.ID, 256) || !validCollectionMetadata(g.Name, 1024) {
			return fmt.Errorf("分组 ID 或名称无效")
		}
		groups[g.ID] = g
	}
	if document.RootGroupID != "" {
		root, ok := groups[document.RootGroupID]
		if !ok || root.ParentID != "" {
			return fmt.Errorf("rootGroupId 必须引用顶层分组")
		}
		if len(GroupSubtree(document.Groups, root.ID)) != len(groups) {
			return fmt.Errorf("集合包含 rootGroupId 子树之外的分组")
		}
	}
	seen := make(map[string]bool, len(document.Requests))
	for _, r := range document.Requests {
		if seen[r.ID] {
			return fmt.Errorf("请求 ID %q 重复", r.ID)
		}
		seen[r.ID] = true
		if r.GroupID != "" {
			if _, ok := groups[r.GroupID]; !ok {
				return fmt.Errorf("请求「%s」引用了不存在的分组 %q", r.Name, r.GroupID)
			}
		} else if document.RootGroupID != "" {
			return fmt.Errorf("请求「%s」不属于 rootGroupId 指定的分组", r.Name)
		}
		if err := validateCollectionRequest(r); err != nil {
			return fmt.Errorf("请求「%s」：%w", r.Name, err)
		}
	}
	for name, vars := range document.Environments {
		if !validCollectionMetadata(name, 1024) || vars == nil {
			return fmt.Errorf("环境「%s」的名称或变量映射无效", name)
		}
		for key, value := range vars {
			if !validCollectionMetadata(key, 1024) || !utf8.ValidString(value) {
				return fmt.Errorf("环境「%s」的变量名称或编码无效", name)
			}
		}
	}
	if document.Environment != "" {
		if _, ok := document.Environments[document.Environment]; !ok {
			return fmt.Errorf("当前 environment 指定的环境不存在")
		}
	}
	return nil
}

func validateCollectionRequest(r Request) error {
	if !validCollectionMetadata(r.ID, 256) || !validCollectionMetadata(r.Name, 1024) {
		return fmt.Errorf("请求 ID 或名称无效")
	}
	if r.Protocol != "HTTP" && r.Protocol != "SSE" && r.Protocol != "WebSocket" {
		return fmt.Errorf("不支持的协议 %q", r.Protocol)
	}
	if !validToken(r.Method) {
		return fmt.Errorf("请求方法无效")
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > 3600 {
		return fmt.Errorf("超时应为 1-3600 秒")
	}
	if r.BodyMode != BodyRaw && r.BodyMode != BodyMultipart {
		return fmt.Errorf("不支持的正文类型 %q", r.BodyMode)
	}
	if r.BodyMode == BodyRaw && len(r.Body) > MaxInput || len(r.URL) > 65536 {
		return fmt.Errorf("请求超过大小限制")
	}
	if !utf8.ValidString(r.Body) || !utf8.ValidString(r.URL) {
		return fmt.Errorf("请求必须使用有效的 UTF-8 编码")
	}
	if err := validateCollectionURL(r.URL, r.Protocol); err != nil {
		return err
	}
	for _, h := range r.Headers {
		if !validToken(strings.TrimSpace(h.Key)) || !validHeaderValue(h.Value) || !utf8.ValidString(h.Value) {
			return fmt.Errorf("请求头名称或值无效")
		}
		if strings.EqualFold(strings.TrimSpace(h.Key), "Content-Length") || strings.EqualFold(strings.TrimSpace(h.Key), "Transfer-Encoding") {
			return fmt.Errorf("Content-Length 和 Transfer-Encoding 由客户端自动计算，请移除它们")
		}
	}
	if err := ValidateRequestAuthentication(r); err != nil {
		return err
	}
	if err := ValidateRequestExtractors(r); err != nil {
		return err
	}
	if err := ValidateRequestAssertions(r); err != nil {
		return err
	}
	if r.Protocol == "WebSocket" && r.BodyMode == BodyMultipart {
		return fmt.Errorf("WebSocket 不支持 multipart 表单")
	}
	if r.BodyMode == BodyMultipart {
		if err := validateFormParts(r.FormParts); err != nil {
			return err
		}
	}
	if len(r.FormParts) > MaxFormParts {
		return fmt.Errorf("表单最多支持 %d 个字段", MaxFormParts)
	}
	formIDs := make(map[string]bool, len(r.FormParts))
	formSize := 0
	for _, p := range r.FormParts {
		if p.ID != "" && (!validCollectionMetadata(p.ID, 256) || formIDs[p.ID]) {
			return fmt.Errorf("表单字段 ID 无效或重复")
		}
		formIDs[p.ID] = true
		if !utf8.ValidString(p.Name) || !utf8.ValidString(p.Value) || !utf8.ValidString(p.FilePath) {
			return fmt.Errorf("表单字段必须使用有效的 UTF-8 编码")
		}
		if p.Kind != FormText && p.Kind != FormFile {
			return fmt.Errorf("不支持的表单字段类型")
		}
		formSize += len(p.Name) + len(p.Value) + len(p.FilePath)
		if formSize > MaxInput {
			return fmt.Errorf("表单字段内容超过 1 MiB")
		}
	}
	size := 0
	queryIDs := make(map[string]bool, len(r.QueryParams))
	for _, p := range r.QueryParams {
		if p.ID != "" && (!validCollectionMetadata(p.ID, 256) || queryIDs[p.ID]) {
			return fmt.Errorf("Query 字段 ID 无效或重复")
		}
		queryIDs[p.ID] = true
		if !utf8.ValidString(p.Key) || !utf8.ValidString(p.Value) || !utf8.ValidString(p.Raw) {
			return fmt.Errorf("Query 编辑信息必须使用有效的 UTF-8 编码")
		}
		size += len(p.Key) + len(p.Value) + len(p.Raw)
		if size > MaxInput {
			return fmt.Errorf("Query 编辑信息超过 1 MiB")
		}
		if p.HasRaw {
			if _, _, err := queryPair(p.Raw); err != nil {
				return fmt.Errorf("Query 编辑信息的原始编码无效：%w", err)
			}
		}
	}
	return nil
}

// Check a structural stand-in, never environment values. A leading variable can
// supply an entire URL or its scheme; other variables stand in for URL components.
func validateCollectionURL(raw, protocol string) error {
	address := strings.TrimSpace(raw)
	scheme := "https"
	if protocol == "WebSocket" {
		scheme = "wss"
	}
	if match := variable.FindStringIndex(address); match != nil && match[0] == 0 {
		suffix := address[match[1]:]
		if strings.HasPrefix(suffix, "://") {
			address = scheme + suffix
		} else if suffix == "" || strings.ContainsAny(suffix[:1], "/?#") {
			address = scheme + "://curlgo.invalid" + suffix
		} else {
			// The prefix can include its own trailing slash, and the suffix
			// can be another variable. Preserve both in the saved template.
			address = scheme + "://curlgo.invalid/" + suffix
		}
	}
	address = variable.ReplaceAllString(address, "1")
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("请求地址无效")
	}
	if u.User != nil {
		return fmt.Errorf("请在 Authorization 请求头中填写认证信息")
	}
	if protocol == "WebSocket" {
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return fmt.Errorf("WebSocket 地址应以 ws:// 或 wss:// 开头")
		}
	} else if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("HTTP/SSE 地址应以 http:// 或 https:// 开头")
	}
	if _, err := ParseQueryParams(address); err != nil {
		return err
	}
	return nil
}
