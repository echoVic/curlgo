package core

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type AssertionSource string
type AssertionOperator string

const (
	AssertStatus   AssertionSource   = "status"
	AssertHeader   AssertionSource   = "header"
	AssertJSON     AssertionSource   = "json"
	AssertBody     AssertionSource   = "body"
	AssertEquals   AssertionOperator = "equals"
	AssertContains AssertionOperator = "contains"
	AssertExists   AssertionOperator = "exists"
	MaxAssertions                    = 100
)

type ResponseAssertion struct {
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name,omitempty"`
	Source   AssertionSource   `json:"source"`
	Operator AssertionOperator `json:"operator"`
	Selector string            `json:"selector,omitempty"`
	Expected string            `json:"expected,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

type AssertionResult struct {
	Index   int    `json:"index"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Passed  bool   `json:"passed"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AssertionReport struct {
	Passed  bool              `json:"passed"`
	Results []AssertionResult `json:"results"`
}

type AssertionConfigError struct{}

func (*AssertionConfigError) Error() string {
	return "断言规则无效，请检查来源、操作、选择器和期望值"
}

func ValidateAssertions(rules []ResponseAssertion) error {
	invalid := func() error { return &AssertionConfigError{} }
	if len(rules) > MaxAssertions {
		return invalid()
	}
	ids, size := map[string]bool{}, 0
	for _, r := range rules {
		size += len(r.ID) + len(r.Name) + len(r.Source) + len(r.Operator) + len(r.Selector) + len(r.Expected)
		if size > MaxInput || len(r.ID) > 256 || len(r.Name) > 256 || len(r.Selector) > 4096 || len(r.Expected) > 65536 {
			return invalid()
		}
		for _, text := range []string{r.ID, r.Name, string(r.Source), string(r.Operator), r.Selector, r.Expected} {
			if !utf8.ValidString(text) {
				return invalid()
			}
		}
		if strings.ContainsFunc(r.Name, unicode.IsControl) {
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
		if r.Operator != AssertEquals && r.Operator != AssertContains && r.Operator != AssertExists {
			return invalid()
		}
		switch r.Source {
		case AssertStatus:
			status, err := strconv.Atoi(r.Expected)
			if r.Operator != AssertEquals || r.Selector != "" || err != nil || status < 100 || status > 599 || strconv.Itoa(status) != r.Expected {
				return invalid()
			}
		case AssertHeader:
			if !validToken(r.Selector) {
				return invalid()
			}
		case AssertJSON:
			if _, ok := pointerTokens(r.Selector); !ok {
				return invalid()
			}
			if r.Operator != AssertExists {
				expected, ok := decodeResponseJSON([]byte(r.Expected))
				if !ok || !isJSONScalar(expected) {
					return invalid()
				}
				if _, ok := expected.(string); r.Operator == AssertContains && !ok {
					return invalid()
				}
			}
		case AssertBody:
			if r.Selector != "" || r.Operator == AssertExists {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}

func ValidateRequestAssertions(r Request) error {
	if err := ValidateAssertions(r.Assertions); err != nil {
		return err
	}
	for _, rule := range r.Assertions {
		if !rule.Disabled && r.Protocol != "HTTP" {
			return &AssertionConfigError{}
		}
	}
	return nil
}

// EvaluateAssertions evaluates every enabled rule. Callers must supply a complete
// response from a successful transport. Feedback never includes response values.
func EvaluateAssertions(rules []ResponseAssertion, status int, headers http.Header, body []byte, truncated bool) (AssertionReport, error) {
	report := AssertionReport{Passed: true}
	if err := ValidateAssertions(rules); err != nil {
		return report, err
	}
	var document any
	jsonValid := false
	if !truncated {
		for _, r := range rules {
			if !r.Disabled && r.Source == AssertJSON {
				document, jsonValid = decodeResponseJSON(body)
				break
			}
		}
	}
	for i, r := range rules {
		if r.Disabled {
			continue
		}
		code := "TRUNCATED_RESPONSE"
		if !truncated {
			code = evaluateAssertion(r, status, headers, body, document, jsonValid)
		}
		passed := code == "PASSED"
		report.Passed = report.Passed && passed
		report.Results = append(report.Results, AssertionResult{
			Index: i, ID: r.ID, Name: r.Name, Passed: passed, Code: code, Message: assertionMessage(code),
		})
	}
	return report, nil
}

func evaluateAssertion(r ResponseAssertion, status int, headers http.Header, body []byte, document any, jsonValid bool) string {
	switch r.Source {
	case AssertStatus:
		return assertionMatch(strconv.Itoa(status) == r.Expected)
	case AssertHeader:
		values := responseHeaderValues(headers, r.Selector)
		if len(values) == 0 {
			return "MISSING_VALUE"
		}
		if r.Operator == AssertExists {
			return "PASSED"
		}
		if len(values) != 1 {
			return "AMBIGUOUS_HEADER"
		}
		return compareAssertionText(values[0], r.Expected, r.Operator)
	case AssertBody:
		if !utf8.Valid(body) {
			return "INVALID_BODY"
		}
		return compareAssertionText(string(body), r.Expected, r.Operator)
	case AssertJSON:
		if !jsonValid {
			return "INVALID_JSON"
		}
		tokens, _ := pointerTokens(r.Selector)
		value, exists := lookupJSONPointer(document, tokens)
		if !exists {
			return "MISSING_VALUE"
		}
		if r.Operator == AssertExists {
			return "PASSED"
		}
		if !isJSONScalar(value) {
			return "NON_SCALAR"
		}
		expected, _ := decodeResponseJSON([]byte(r.Expected)) // validated before evaluation
		switch actual := value.(type) {
		case string:
			want, ok := expected.(string)
			if !ok {
				return "TYPE_MISMATCH"
			}
			return compareAssertionText(actual, want, r.Operator)
		case json.Number:
			want, ok := expected.(json.Number)
			if !ok || r.Operator != AssertEquals {
				return "TYPE_MISMATCH"
			}
			return assertionMatch(normalizeJSONNumber(actual) == normalizeJSONNumber(want))
		case bool:
			want, ok := expected.(bool)
			if !ok || r.Operator != AssertEquals {
				return "TYPE_MISMATCH"
			}
			return assertionMatch(actual == want)
		default: // null
			if expected != nil || r.Operator != AssertEquals {
				return "TYPE_MISMATCH"
			}
			return "PASSED"
		}
	}
	return "MISMATCH"
}

func isJSONScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number:
		return true
	default:
		return false
	}
}

func compareAssertionText(actual, expected string, op AssertionOperator) string {
	if op == AssertContains {
		return assertionMatch(strings.Contains(actual, expected))
	}
	return assertionMatch(actual == expected)
}

func assertionMatch(passed bool) string {
	if passed {
		return "PASSED"
	}
	return "MISMATCH"
}

func assertionMessage(code string) string {
	switch code {
	case "PASSED":
		return "断言通过"
	case "MISSING_VALUE":
		return "未找到字段或响应头"
	case "INVALID_JSON":
		return "响应不是完整有效的 UTF-8 JSON"
	case "NON_SCALAR":
		return "相等比较仅支持字符串、数字、布尔值和 null"
	case "TYPE_MISMATCH":
		return "值的类型与期望不符"
	case "AMBIGUOUS_HEADER":
		return "响应头包含多个值，无法确定比较结果"
	case "TRUNCATED_RESPONSE":
		return "响应已截断，无法验证"
	case "INVALID_BODY":
		return "响应正文不是有效的 UTF-8 文本"
	default:
		return "值与期望不符"
	}
}

// Canonical decimal digits and exponent avoid both float rounding and allocating
// enormous powers of ten for valid JSON numbers with very large exponents.
func normalizeJSONNumber(number json.Number) string {
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(number.String()), "e")
	scale := new(big.Int)
	if hasExponent {
		scale.SetString(exponent, 10)
	}
	negative := strings.HasPrefix(mantissa, "-")
	mantissa = strings.TrimPrefix(mantissa, "-")
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		scale.Sub(scale, big.NewInt(int64(len(mantissa)-dot-1)))
		mantissa = mantissa[:dot] + mantissa[dot+1:]
	}
	mantissa = strings.TrimLeft(mantissa, "0")
	if mantissa == "" {
		return "0"
	}
	digits := strings.TrimRight(mantissa, "0")
	scale.Add(scale, big.NewInt(int64(len(mantissa)-len(digits))))
	if negative {
		digits = "-" + digits
	}
	return digits + "e" + scale.String()
}
