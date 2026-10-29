package core

import (
	"fmt"
	"net/url"
	"strings"
)

// QueryParam is editor metadata. The URL remains the source of active parameters.
// Raw preserves untouched escaping, bare flags and empty query segments.
type QueryParam struct {
	ID       string `json:"id,omitempty"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
	Raw      string `json:"raw,omitempty"`
	HasRaw   bool   `json:"hasRaw,omitempty"`
}

func splitQuery(address string) (prefix, query, fragment string, hasQuery bool) {
	before, anchor, hasFragment := strings.Cut(address, "#")
	if hasFragment {
		fragment = "#" + anchor
	}
	prefix, query, hasQuery = strings.Cut(before, "?")
	return
}

func queryPair(raw string) (key, value string, err error) {
	key, value, _ = strings.Cut(raw, "=")
	if key, err = url.QueryUnescape(key); err != nil {
		return
	}
	value, err = url.QueryUnescape(value)
	return
}

func ParseQueryParams(address string) ([]QueryParam, error) {
	_, query, _, _ := splitQuery(address)
	if query == "" {
		return nil, nil
	}
	var params []QueryParam
	for _, raw := range strings.Split(query, "&") {
		key, value, err := queryPair(raw)
		if err != nil {
			return nil, fmt.Errorf("Query 参数编码无效，请先修正请求地址：%w", err)
		}
		params = append(params, QueryParam{ID: NewID(), Key: key, Value: value, Raw: raw, HasRaw: true})
	}
	return params, nil
}

// Escape text while keeping variable placeholders editable until Resolve.
func escapeQueryTemplate(text string) string {
	var out strings.Builder
	start := 0
	for _, match := range variable.FindAllStringIndex(text, -1) {
		out.WriteString(url.QueryEscape(text[start:match[0]]))
		out.WriteString(text[match[0]:match[1]])
		start = match[1]
	}
	out.WriteString(url.QueryEscape(text[start:]))
	return out.String()
}

// Encoded returns the current query segment, preserving unchanged components.
// A newly added empty row has no segment yet; placeholders remain unexpanded.
func (p QueryParam) Encoded() (string, bool) {
	if !p.HasRaw && p.Key == "" && p.Value == "" {
		return "", false
	}
	key, value := escapeQueryTemplate(p.Key), escapeQueryTemplate(p.Value)
	hasValue := true
	if p.HasRaw {
		oldKey, oldValue, err := queryPair(p.Raw)
		rawKey, rawValue, hadValue := strings.Cut(p.Raw, "=")
		if err == nil {
			if oldKey == p.Key {
				key = rawKey
			}
			if oldValue == p.Value {
				value, hasValue = rawValue, hadValue
			}
		}
	}
	if hasValue {
		return key + "=" + value, true
	}
	return key, true
}

func WithQueryParams(address string, params []QueryParam) string {
	prefix, original, fragment, hasQuery := splitQuery(address)
	var segments []string
	for _, p := range params {
		if p.Disabled {
			continue
		}
		if segment, present := p.Encoded(); present {
			segments = append(segments, segment)
		}
	}
	if len(segments) != 0 {
		return prefix + "?" + strings.Join(segments, "&") + fragment
	}
	if hasQuery && original == "" {
		return prefix + "?" + fragment
	}
	return prefix + fragment
}

func expandURL(address string, vars map[string]string) (string, error) {
	prefix, query, fragment, hasQuery := splitQuery(address)
	prefix, err := Expand(prefix, vars)
	if err != nil {
		return "", err
	}
	fragment, err = Expand(fragment, vars)
	if err != nil {
		return "", err
	}
	if !hasQuery {
		return prefix + fragment, nil
	}
	escaped := make(map[string]string, len(vars))
	for key, value := range vars {
		escaped[key] = url.QueryEscape(value)
	}
	query, err = Expand(query, escaped)
	if err != nil {
		return "", err
	}
	return prefix + "?" + query + fragment, nil
}
