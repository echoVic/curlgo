package core

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CollectionImport holds request templates parsed from another API client.
// Nothing is sent or saved until the caller confirms; Warnings must be shown
// first because they list skipped requests and imported file references.
type CollectionImport struct {
	Name         string
	Requests     []Request
	Groups       []RequestGroup
	RootGroupID  string
	Environment  string
	Environments map[string]map[string]string
	Warnings     []string
}

// MaxImportRequests bounds a single import so a huge collection cannot freeze the UI.
const MaxImportRequests = 1000

func (c *CollectionImport) warn(format string, args ...any) {
	if len(c.Warnings) < 200 {
		c.Warnings = append(c.Warnings, fmt.Sprintf(format, args...))
	}
}

var defaultContentTypes = map[string]string{
	"json": "application/json", "xml": "application/xml", "text": "text/plain",
	"html": "text/html", "javascript": "application/javascript",
	"form": "application/x-www-form-urlencoded",
}

// withDefaultContentType mirrors Postman and Bruno, which only add a body's
// Content-Type when the request does not declare one.
func withDefaultContentType(headers []Pair, contentType string) []Pair {
	if contentType == "" {
		return headers
	}
	for _, h := range headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			return headers
		}
	}
	return append(headers, Pair{"Content-Type", contentType})
}

type formField struct{ key, value string }

// encodeForm percent-encodes fields like Postman and Bruno, except that
// {{variables}} are kept intact so they still resolve when sending. Values
// substituted later are sent as-is, so the caller warns about them.
func encodeForm(fields []formField) (string, bool) {
	escape := func(s string) string {
		var b strings.Builder
		last := 0
		for _, m := range variable.FindAllStringIndex(s, -1) {
			b.WriteString(url.QueryEscape(s[last:m[0]]))
			b.WriteString(s[m[0]:m[1]])
			last = m[1]
		}
		b.WriteString(url.QueryEscape(s[last:]))
		return b.String()
	}
	parts := make([]string, 0, len(fields))
	hasVariable := false
	for _, f := range fields {
		hasVariable = hasVariable || variable.MatchString(f.key) || variable.MatchString(f.value)
		parts = append(parts, escape(f.key)+"="+escape(f.value))
	}
	return strings.Join(parts, "&"), hasVariable
}

// graphQLBody builds the JSON body both clients send for GraphQL requests.
func graphQLBody(query, variables string) (string, error) {
	vars := json.RawMessage("{}")
	if strings.TrimSpace(variables) != "" {
		if !json.Valid([]byte(variables)) {
			return "", fmt.Errorf("GraphQL 变量不是有效的 JSON")
		}
		vars = json.RawMessage(strings.TrimSpace(variables))
	}
	q, err := json.Marshal(query)
	if err != nil {
		return "", err
	}
	return `{"query":` + string(q) + `,"variables":` + string(vars) + `}`, nil
}

// substitutePathParams replaces whole ":name" path segments, as Postman and
// Bruno do when sending. Unknown names and empty values keep the placeholder.
func substitutePathParams(raw string, values map[string]string) string {
	if len(values) == 0 {
		return raw
	}
	end := len(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		end = i
	}
	start := 0
	if i := strings.Index(raw[:end], "://"); i >= 0 {
		start = i + 3
	}
	segments := strings.Split(raw[start:end], "/")
	for i := 1; i < len(segments); i++ {
		if name, ok := strings.CutPrefix(segments[i], ":"); ok {
			if value := values[name]; value != "" {
				segments[i] = value
			}
		}
	}
	return raw[:start] + strings.Join(segments, "/") + raw[end:]
}

// checkImported applies the checks Resolve would fail on later, so a request
// that could never be sent is skipped with a reason instead of imported.
func checkImported(r Request) error {
	if !validToken(r.Method) {
		return fmt.Errorf("请求方法 %q 无效", r.Method)
	}
	if strings.TrimSpace(r.URL) == "" {
		return fmt.Errorf("缺少请求地址")
	}
	if len(r.URL) > 65536 || len(r.Body) > MaxInput {
		return fmt.Errorf("请求超过大小限制")
	}
	if !variable.MatchString(r.URL) {
		u, err := url.Parse(r.URL)
		if err != nil || u.Hostname() == "" {
			return fmt.Errorf("请求地址无效")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("不支持 %s 协议", u.Scheme)
		}
	}
	for _, h := range r.Headers {
		if !validToken(h.Key) {
			return fmt.Errorf("请求头名称 %q 无效", h.Key)
		}
		if !validHeaderValue(h.Value) {
			return fmt.Errorf("请求头 %s 的值包含换行或控制字符", h.Key)
		}
		if strings.EqualFold(h.Key, "Content-Length") || strings.EqualFold(h.Key, "Transfer-Encoding") {
			return fmt.Errorf("请求头 %s 由客户端自动计算", h.Key)
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
	if r.BodyMode == BodyMultipart {
		return validateFormParts(r.FormParts)
	}
	return nil
}
