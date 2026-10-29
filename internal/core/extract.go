package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ExtractionSource string

const (
	ExtractJSON   ExtractionSource = "json"
	ExtractHeader ExtractionSource = "header"
	MaxExtractors                  = 100
)

type ResponseExtractor struct {
	ID       string           `json:"id,omitempty"`
	Variable string           `json:"variable"`
	Source   ExtractionSource `json:"source"`
	Selector string           `json:"selector"`
	Disabled bool             `json:"disabled,omitempty"`
}

type ExtractionErrorCode string

const (
	ExtractionInvalidRules ExtractionErrorCode = "INVALID_RULES"
	ExtractionInvalidJSON  ExtractionErrorCode = "INVALID_JSON"
	ExtractionMissing      ExtractionErrorCode = "MISSING_VALUE"
	ExtractionNonScalar    ExtractionErrorCode = "NON_SCALAR"
	ExtractionAmbiguous    ExtractionErrorCode = "AMBIGUOUS_HEADER"
	ExtractionTruncated    ExtractionErrorCode = "TRUNCATED_RESPONSE"
	ExtractionTooLarge     ExtractionErrorCode = "VALUE_TOO_LARGE"
)

// ExtractionError never includes response data, selectors, or extracted values.
type ExtractionError struct {
	Variable string
	Code     ExtractionErrorCode
}

func (e *ExtractionError) Error() string {
	messages := map[ExtractionErrorCode]string{
		ExtractionInvalidRules: "提取规则无效，请检查变量名、来源和选择器",
		ExtractionInvalidJSON:  "响应正文不是完整有效的 UTF-8 JSON",
		ExtractionMissing:      "未找到要提取的字段或响应头",
		ExtractionNonScalar:    "仅可提取字符串、数字或布尔值",
		ExtractionAmbiguous:    "响应头包含多个值，无法确定提取结果",
		ExtractionTruncated:    "响应已截断，无法提取变量",
		ExtractionTooLarge:     "提取的变量超过 1 MiB",
	}
	message := messages[e.Code]
	if e.Variable != "" {
		return fmt.Sprintf("变量 %s：%s", e.Variable, message)
	}
	return message
}

var extractionVariable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func ValidateExtractors(rules []ResponseExtractor) error {
	invalid := func() error { return &ExtractionError{Code: ExtractionInvalidRules} }
	if len(rules) > MaxExtractors {
		return invalid()
	}
	names, ids := map[string]bool{}, map[string]bool{}
	size := 0
	for _, r := range rules {
		size += len(r.ID) + len(r.Variable) + len(r.Source) + len(r.Selector)
		if size > MaxInput || len(r.ID) > 256 || len(r.Variable) > 256 || len(r.Selector) > 4096 ||
			!utf8.ValidString(r.ID) || !utf8.ValidString(r.Variable) || !utf8.ValidString(string(r.Source)) || !utf8.ValidString(r.Selector) {
			return invalid()
		}
		if r.ID != "" {
			if !validToken(r.ID) || ids[r.ID] {
				return invalid()
			}
			ids[r.ID] = true
		}
		if r.Disabled {
			continue
		}
		if !extractionVariable.MatchString(r.Variable) || names[r.Variable] {
			return invalid()
		}
		names[r.Variable] = true
		switch r.Source {
		case ExtractJSON:
			if _, ok := pointerTokens(r.Selector); !ok {
				return invalid()
			}
		case ExtractHeader:
			if !validToken(r.Selector) {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}

func ValidateRequestExtractors(r Request) error {
	if err := ValidateExtractors(r.Extractors); err != nil {
		return err
	}
	for _, rule := range r.Extractors {
		if !rule.Disabled && r.Protocol != "HTTP" {
			return &ExtractionError{Code: ExtractionInvalidRules}
		}
	}
	return nil
}

// ExtractVariables returns the entire enabled batch or no updates. Callers decide
// whether the HTTP status and transport completion permit applying these values.
func ExtractVariables(rules []ResponseExtractor, headers http.Header, body []byte, truncated bool) (map[string]string, error) {
	if err := ValidateExtractors(rules); err != nil {
		return nil, err
	}
	active, needsJSON := false, false
	for _, r := range rules {
		if !r.Disabled {
			active = true
			needsJSON = needsJSON || r.Source == ExtractJSON
		}
	}
	if !active {
		return nil, nil
	}
	if truncated {
		return nil, &ExtractionError{Code: ExtractionTruncated}
	}
	var document any
	if needsJSON {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		var trailing any
		if !utf8.Valid(body) || decoder.Decode(&document) != nil || decoder.Decode(&trailing) != io.EOF {
			return nil, &ExtractionError{Code: ExtractionInvalidJSON}
		}
	}
	values := make(map[string]string)
	size := 0
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		var value string
		var code ExtractionErrorCode
		if r.Source == ExtractJSON {
			tokens, _ := pointerTokens(r.Selector) // validated above
			value, code = extractJSONScalar(document, tokens)
		} else {
			var found []string
			for k, v := range headers {
				if strings.EqualFold(k, r.Selector) {
					found = append(found, v...)
				}
			}
			switch len(found) {
			case 0:
				code = ExtractionMissing
			case 1:
				value = found[0]
			default:
				code = ExtractionAmbiguous
			}
		}
		size += len(r.Variable) + len(value)
		if code == "" && (size > MaxInput || !utf8.ValidString(value)) {
			code = ExtractionTooLarge
		}
		if code != "" {
			return nil, &ExtractionError{Variable: r.Variable, Code: code}
		}
		values[r.Variable] = value
	}
	return values, nil
}

func pointerTokens(pointer string) ([]string, bool) {
	if pointer == "" {
		return nil, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	tokens := strings.Split(pointer[1:], "/")
	for i, token := range tokens {
		var b strings.Builder
		for n := 0; n < len(token); n++ {
			if token[n] != '~' {
				b.WriteByte(token[n])
				continue
			}
			n++
			if n >= len(token) || token[n] != '0' && token[n] != '1' {
				return nil, false
			}
			if token[n] == '0' {
				b.WriteByte('~')
			} else {
				b.WriteByte('/')
			}
		}
		tokens[i] = b.String()
	}
	return tokens, true
}

func extractJSONScalar(value any, tokens []string) (string, ExtractionErrorCode) {
	for _, token := range tokens {
		switch node := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = node[token]
			if !ok {
				return "", ExtractionMissing
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != token {
				return "", ExtractionMissing
			}
			value = node[index]
		default:
			return "", ExtractionMissing
		}
	}
	switch scalar := value.(type) {
	case string:
		return scalar, ""
	case json.Number:
		return scalar.String(), ""
	case bool:
		return strconv.FormatBool(scalar), ""
	default:
		return "", ExtractionNonScalar
	}
}
