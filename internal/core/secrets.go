package core

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const SecretPrefix = "secret."
const MaxSecrets = 1000

type MissingSecretError struct{ Name string }

func (e *MissingSecretError) Error() string {
	return "机密变量不可用，请先解锁并配置当前环境的机密变量"
}

func ValidateSecretValues(values map[string]string) error {
	if len(values) > MaxSecrets {
		return fmt.Errorf("机密变量最多支持 1000 项")
	}
	total := 0
	for name, value := range values {
		if len(name) > 256 || strings.HasPrefix(name, SecretPrefix) || !variable.MatchString("{{"+name+"}}") ||
			variable.FindString("{{"+name+"}}") != "{{"+name+"}}" || strings.TrimSpace(name) != name {
			return fmt.Errorf("机密变量名称无效，请使用不含 secret. 前缀的变量名")
		}
		if !utf8.ValidString(value) || len(value) > 65536 {
			return fmt.Errorf("机密变量值必须为 UTF-8 字符串且不超过 64 KiB")
		}
		total += len(name) + len(value)
	}
	if total > MaxInput {
		return fmt.Errorf("机密变量内容超过 1 MiB")
	}
	return nil
}

// Values are injected into a transient map, never into persisted settings.
func ResolveWithSecrets(r Request, s Settings, secrets map[string]string) (Request, error) {
	if err := ValidateSecretValues(secrets); err != nil {
		return r, err
	}
	vars := publicVariables(s)
	for name, value := range secrets {
		vars[SecretPrefix+name] = value
	}
	return resolve(r, s, vars, false)
}

func publicVariables(s Settings) map[string]string {
	vars := make(map[string]string, len(s.Environments[s.Environment]))
	for name, value := range s.Environments[s.Environment] {
		if !strings.HasPrefix(name, SecretPrefix) {
			vars[name] = value
		}
	}
	return vars
}

// Export resolves ordinary values once and keeps private references as templates.
// It deliberately has no capability to read a vault.
func resolveForExport(r Request, s Settings) (Request, bool, error) {
	if r.Auth.Mode == AuthAPIKey && HasSecretReference(r.Auth.Key) {
		// Curl has no API Key ownership operation. Without its name we cannot
		// remove shadowed headers/parameters without consulting private values.
		return r, false, fmt.Errorf("复制 curl 需要公开的 API Key 名称，请将机密引用用于值")
	}
	prefix, query, _, _ := splitQuery(r.URL)
	if r.Auth.Mode == AuthAPIKey && r.Auth.Location == APIKeyQuery {
		if HasSecretReference(prefix) {
			return r, false, fmt.Errorf("复制 Query 鉴权需要公开的 URL 前缀，请将机密引用用于参数值")
		}
		for _, segment := range strings.Split(query, "&") {
			key, _, _ := strings.Cut(segment, "=")
			key, err := url.QueryUnescape(key)
			if err == nil && HasSecretReference(key) {
				return r, false, fmt.Errorf("复制 Query 鉴权需要公开的参数名称，请将机密引用用于值")
			}
		}
	}
	authHeader := r.Auth.Mode == AuthBearer || r.Auth.Mode == AuthBasic || r.Auth.Mode == AuthAPIKey && r.Auth.Location != APIKeyQuery
	if len(r.Headers) > 0 && len(s.GlobalHeaders) > 0 || authHeader {
		for _, headers := range [][]Pair{r.Headers, s.GlobalHeaders} {
			for _, h := range headers {
				if HasSecretReference(h.Key) {
					return r, false, fmt.Errorf("复制 curl 时混合全局 Header 或鉴权需要公开的 Header 名称")
				}
			}
		}
	}
	vars := publicVariables(s)
	preserve := false
	markers := map[string]string{}
	for _, text := range requestVariableFields(r, s) {
		for _, match := range variable.FindAllStringSubmatch(text, -1) {
			name := match[1]
			if strings.HasPrefix(name, SecretPrefix) {
				if err := ValidateSecretValues(map[string]string{strings.TrimPrefix(name, SecretPrefix): ""}); err != nil {
					return r, false, err
				}
				if _, exists := markers[name]; !exists {
					markers[name] = "{{curlgo_private_" + NewID() + "}}"
				}
				vars[name] = markers[name]
				preserve = true
			}
		}
	}
	basicPrivate := r.Auth.Mode == AuthBasic && (HasSecretReference(r.Auth.Username) || HasSecretReference(r.Auth.Password))
	resolved, err := resolve(r, s, vars, preserve)
	if err == nil && preserve && exportHasLiteralTemplate(resolved, markers) {
		return r, false, fmt.Errorf("替换值包含字面的变量语法，无法在 curl 中安全保留")
	}
	if err == nil {
		resolved = restoreExportReferences(resolved, markers)
	}
	return resolved, basicPrivate, err
}

func exportHasLiteralTemplate(r Request, markers map[string]string) bool {
	allowed := map[string]bool{}
	for _, marker := range markers {
		allowed[marker] = true
	}
	fields := []string{r.URL}
	if r.BodyMode == BodyRaw {
		fields = append(fields, r.Body)
	} else {
		for _, part := range r.FormParts {
			if part.Kind == FormText {
				fields = append(fields, part.Value)
			}
		}
	}
	for _, h := range r.Headers {
		fields = append(fields, h.Key, h.Value)
	}
	if r.Auth.Mode == AuthBasic {
		fields = append(fields, r.Auth.Username, r.Auth.Password)
	}
	for _, field := range fields {
		for _, match := range variable.FindAllString(field, -1) {
			if !allowed[match] {
				return true
			}
		}
	}
	return false
}

func requestVariableFields(r Request, s Settings) []string {
	fields := []string{r.URL, r.Body, r.Auth.Token, r.Auth.Username, r.Auth.Password, r.Auth.Key, r.Auth.Value}
	for _, h := range r.Headers {
		fields = append(fields, h.Key, h.Value)
	}
	for _, h := range s.GlobalHeaders {
		fields = append(fields, h.Key, h.Value)
	}
	for _, p := range r.FormParts {
		if p.Kind == FormText {
			fields = append(fields, p.Value)
		}
	}
	return fields
}

func HasSecretReference(text string) bool {
	for _, match := range variable.FindAllStringSubmatch(text, -1) {
		if strings.HasPrefix(match[1], SecretPrefix) {
			return true
		}
	}
	return false
}

func RequestUsesSecrets(r Request, s Settings) bool {
	for _, text := range requestVariableFields(r, s) {
		if HasSecretReference(text) {
			return true
		}
	}
	return false
}

func restoreExportReferences(r Request, markers map[string]string) Request {
	restore := func(text string, query bool) string {
		for name, marker := range markers {
			if query {
				text = strings.ReplaceAll(text, url.QueryEscape(marker), "{{"+name+"}}")
			}
			text = strings.ReplaceAll(text, marker, "{{"+name+"}}")
		}
		return text
	}
	r.URL = restore(r.URL, true)
	r.Body = restore(r.Body, false)
	for _, field := range []*string{&r.Auth.Token, &r.Auth.Username, &r.Auth.Password, &r.Auth.Key, &r.Auth.Value} {
		*field = restore(*field, false)
	}
	for i := range r.Headers {
		r.Headers[i].Key = restore(r.Headers[i].Key, false)
		r.Headers[i].Value = restore(r.Headers[i].Value, false)
	}
	r.FormParts = append([]FormPart(nil), r.FormParts...)
	for i := range r.FormParts {
		r.FormParts[i].Value = restore(r.FormParts[i].Value, false)
	}
	return r
}
