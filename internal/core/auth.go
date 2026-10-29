package core

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type AuthMode string
type APIKeyLocation string

const (
	AuthManual AuthMode = ""
	AuthBearer AuthMode = "bearer"
	AuthBasic  AuthMode = "basic"
	AuthAPIKey AuthMode = "apikey"

	APIKeyHeader APIKeyLocation = "header"
	APIKeyQuery  APIKeyLocation = "query"
)

// Authentication stores editable templates. Credentials are expanded only by Resolve.
type Authentication struct {
	Mode     AuthMode       `json:"mode,omitempty"`
	Token    string         `json:"token,omitempty"`
	Username string         `json:"username,omitempty"`
	Password string         `json:"password,omitempty"`
	Key      string         `json:"key,omitempty"`
	Value    string         `json:"value,omitempty"`
	Location APIKeyLocation `json:"location,omitempty"`
}

// ValidateAuthentication checks saved templates without consulting environments.
func ValidateAuthentication(a Authentication) error {
	if err := validateAuthenticationSize(a); err != nil {
		return err
	}
	a.Token = variable.ReplaceAllString(a.Token, "x")
	a.Username = variable.ReplaceAllString(a.Username, "x")
	a.Password = variable.ReplaceAllString(a.Password, "x")
	a.Key = variable.ReplaceAllString(a.Key, "x")
	a.Value = variable.ReplaceAllString(a.Value, "x")
	return validateActiveAuthentication(a)
}

// ValidateRequestAuthentication also checks headers owned by the body encoder.
func ValidateRequestAuthentication(r Request) error {
	if err := ValidateAuthentication(r.Auth); err != nil {
		return err
	}
	return validateAuthenticationBody(r.Auth, r.BodyMode)
}

func validateAuthenticationBody(a Authentication, mode BodyMode) error {
	if mode == BodyMultipart && a.Mode == AuthAPIKey &&
		(a.Location == "" || a.Location == APIKeyHeader) &&
		strings.EqualFold(strings.TrimSpace(a.Key), "Content-Type") {
		return fmt.Errorf("multipart 的 Content-Type 由客户端自动生成，不能用作 API Key")
	}
	return nil
}

func validateAuthenticationSize(a Authentication) error {
	size := 0
	for _, value := range []string{string(a.Mode), a.Token, a.Username, a.Password, a.Key, a.Value, string(a.Location)} {
		size += len(value)
		if !utf8.ValidString(value) {
			return fmt.Errorf("鉴权配置必须使用有效的 UTF-8 编码")
		}
	}
	if size > MaxInput {
		return fmt.Errorf("鉴权配置超过 1 MiB")
	}
	return nil
}

func resolveAuthentication(a Authentication, vars map[string]string) (Authentication, error) {
	return resolveAuthenticationMode(a, vars, false)
}

func resolveAuthenticationMode(a Authentication, vars map[string]string, exporting bool) (Authentication, error) {
	if err := ValidateAuthentication(a); err != nil {
		return a, err
	}
	var fields []*string
	switch a.Mode {
	case AuthManual:
		return a, nil
	case AuthBearer:
		fields = []*string{&a.Token}
	case AuthBasic:
		fields = []*string{&a.Username, &a.Password}
	case AuthAPIKey:
		fields = []*string{&a.Key, &a.Value}
	default:
		return a, fmt.Errorf("不支持的鉴权类型")
	}
	for _, field := range fields {
		value, err := Expand(*field, vars)
		if err != nil {
			return a, err
		}
		*field = value
	}
	if err := validateAuthenticationSize(a); err != nil {
		return a, err
	}
	validate := validateActiveAuthentication
	if exporting {
		validate = ValidateAuthentication
	}
	if err := validate(a); err != nil {
		return a, err
	}
	return a, nil
}

func validateActiveAuthentication(a Authentication) error {
	switch a.Mode {
	case AuthManual:
	case AuthBearer:
		if strings.TrimSpace(a.Token) == "" || !validHeaderValue(a.Token) {
			return fmt.Errorf("Bearer Token 不能为空或包含控制字符")
		}
	case AuthBasic:
		if strings.Contains(a.Username, ":") || !validHeaderValue(a.Username) || !validHeaderValue(a.Password) {
			return fmt.Errorf("Basic 用户名不能包含冒号，凭证不能包含控制字符")
		}
	case AuthAPIKey:
		switch a.Location {
		case "", APIKeyHeader:
			key := strings.TrimSpace(a.Key)
			if !validToken(key) || !validHeaderValue(a.Value) {
				return fmt.Errorf("API Key 请求头名称或值无效")
			}
			if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") {
				return fmt.Errorf("API Key 不能使用客户端自动计算的请求头")
			}
		case APIKeyQuery:
			if strings.TrimSpace(a.Key) == "" {
				return fmt.Errorf("API Key 参数名称不能为空")
			}
		default:
			return fmt.Errorf("不支持的 API Key 位置")
		}
	default:
		return fmt.Errorf("不支持的鉴权类型")
	}
	return nil
}

func authenticationHeader(a Authentication) (Pair, bool) {
	switch a.Mode {
	case AuthBearer:
		return Pair{"Authorization", "Bearer " + a.Token}, true
	case AuthBasic:
		return Pair{"Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte(a.Username+":"+a.Password))}, true
	case AuthAPIKey:
		if a.Location == "" || a.Location == APIKeyHeader {
			return Pair{strings.TrimSpace(a.Key), a.Value}, true
		}
	}
	return Pair{}, false
}

// Set one credential parameter while retaining unrelated raw escaping and order.
func withAPIKeyQuery(address, key, value string) (string, error) {
	out, replaced, err := replaceAPIKeyQuery(address, key, value, nil)
	if err != nil || replaced {
		return out, err
	}
	prefix, query, fragment, _ := splitQuery(address)
	if query != "" {
		query += "&"
	}
	return prefix + "?" + query + url.QueryEscape(key) + "=" + url.QueryEscape(value) + fragment, nil
}

func replaceAPIKeyQuery(address, key, value string, vars map[string]string) (string, bool, error) {
	prefix, query, fragment, _ := splitQuery(address)
	pair := url.QueryEscape(key) + "=" + url.QueryEscape(value)
	escaped := make(map[string]string, len(vars))
	for name, text := range vars {
		escaped[name] = url.QueryEscape(text)
	}
	var segments []string
	replaced := false
	if query != "" {
		for _, raw := range strings.Split(query, "&") {
			rawKey, _, _ := strings.Cut(raw, "=")
			if vars != nil {
				var err error
				rawKey, err = Expand(rawKey, escaped)
				if err != nil {
					return "", false, err
				}
			}
			name, err := url.QueryUnescape(rawKey)
			if err != nil {
				return "", false, fmt.Errorf("API Key 所在地址的 Query 编码无效")
			}
			if name != key {
				if _, _, err := queryPair(raw); err != nil {
					return "", false, fmt.Errorf("API Key 所在地址的 Query 编码无效")
				}
				segments = append(segments, raw)
			} else if !replaced {
				segments = append(segments, pair)
				replaced = true
			}
		}
	}
	if !replaced {
		return address, false, nil
	}
	return prefix + "?" + strings.Join(segments, "&") + fragment, true, nil
}

type authenticationError struct {
	cause error
	text  string
}

func (e *authenticationError) Error() string { return e.text }
func (e *authenticationError) Unwrap() error { return e.cause }

// url.Error includes the sent URL. Mask query credentials while retaining error causes.
func redactAuthenticationError(err error, a Authentication) error {
	if err == nil || a.Mode != AuthAPIKey || a.Location != APIKeyQuery || a.Value == "" {
		return err
	}
	text := err.Error()
	for _, secret := range []string{url.QueryEscape(a.Value), url.PathEscape(a.Value), a.Value} {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	if text == err.Error() {
		return err
	}
	return &authenticationError{cause: err, text: text}
}
