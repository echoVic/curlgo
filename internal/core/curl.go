package core

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// SplitShell reads a single POSIX-style command without executing shell syntax.
func SplitShell(s string) ([]string, error) {
	if strings.ContainsRune(s, 0) {
		return nil, fmt.Errorf("curl 包含无效字符")
	}
	var words []string
	var b strings.Builder
	var quote byte
	active := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' {
			if i+1 >= len(s) {
				return nil, fmt.Errorf("curl 末尾转义不完整")
			}
			next := s[i+1]
			if quote == '"' && next != '"' && next != '\\' && next != '$' && next != '`' && next != '\n' {
				b.WriteByte(ch)
				continue
			}
			i++
			if next != '\n' {
				b.WriteByte(next)
				active = true
			}
			continue
		}
		if ch == '$' || ch == '`' {
			return nil, fmt.Errorf("不执行 shell 展开，请用单引号包住字面内容")
		}
		if quote == '"' {
			if ch == '"' {
				quote = 0
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			active = true
		case ' ', '\t', '\r', '\n':
			if active {
				words = append(words, b.String())
				b.Reset()
				active = false
			}
		case ';', '|', '&', '<', '>', '(', ')':
			return nil, fmt.Errorf("仅支持单条 curl，请将 URL 和正文用引号包住")
		default:
			if ch == 0 {
				return nil, fmt.Errorf("curl 包含无效字符")
			}
			b.WriteByte(ch)
			active = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("curl 引号未闭合")
	}
	if active {
		words = append(words, b.String())
	}
	return words, nil
}

func ImportCURL(input string) (Request, error) {
	r := NewRequest()
	r.FollowRedirects = false
	if len(input) > MaxInput {
		return r, fmt.Errorf("curl 内容超过 1 MiB")
	}
	a, err := SplitShell(input)
	if err != nil {
		return r, err
	}
	if len(a) == 0 || a[0] != "curl" {
		return r, fmt.Errorf("请粘贴以 curl 开头的命令")
	}
	var data []string
	explicitMethod := false
	jsonBody := false
	hasBody := false
	for i := 1; i < len(a); i++ {
		arg := a[i]
		name := arg
		val := ""
		attached := false
		if strings.HasPrefix(arg, "--") {
			name, val, attached = strings.Cut(arg, "=")
		} else if len(arg) > 2 && strings.ContainsRune("XHdubAe", rune(arg[1])) {
			name = arg[:2]
			val = arg[2:]
			attached = true
		}
		take := func() (string, error) {
			if attached {
				return val, nil
			}
			if i+1 >= len(a) {
				return "", fmt.Errorf("%s 缺少参数", name)
			}
			i++
			return a[i], nil
		}
		switch name {
		case "-X", "--request":
			r.Method, err = take()
			explicitMethod = true
		case "-H", "--header":
			var v string
			v, err = take()
			if err == nil {
				var p []Pair
				p, err = ParseHeaders(v)
				if err == nil {
					r.Headers = append(r.Headers, p...)
				}
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--json":
			if r.BodyMode == BodyMultipart {
				return r, fmt.Errorf("curl 不能混用原始正文和 multipart 表单")
			}
			var v string
			v, err = take()
			if err == nil {
				if strings.HasPrefix(v, "@") && name != "--data-raw" {
					err = fmt.Errorf("暂不支持 @file，请直接粘贴正文")
				} else {
					data = append(data, v)
					hasBody = true
					jsonBody = jsonBody || name == "--json"
				}
			}
		case "--form-string":
			if hasBody {
				return r, fmt.Errorf("curl 不能混用原始正文和 multipart 表单")
			}
			var v string
			v, err = take()
			if err == nil {
				key, value, ok := strings.Cut(v, "=")
				if !ok {
					return r, fmt.Errorf("表单字段应为 name=value")
				}
				r.BodyMode = BodyMultipart
				r.FormParts = append(r.FormParts, FormPart{ID: NewID(), Kind: FormText, Name: key, Value: value})
				err = validateFormParts(r.FormParts)
			}
		case "-F", "--form":
			return r, fmt.Errorf("请在正文页选择上传文件；curl 导入不会读取本地文件")
		case "-u", "--user":
			var v string
			v, err = take()
			if !strings.Contains(v, ":") {
				v += ":"
			}
			r.Headers = append(r.Headers, Pair{"Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte(v))})
		case "--oauth2-bearer":
			var v string
			v, err = take()
			r.Headers = append(r.Headers, Pair{"Authorization", "Bearer " + v})
		case "-b", "--cookie":
			var v string
			v, err = take()
			if !strings.Contains(v, "=") {
				err = fmt.Errorf("暂不导入 Cookie 文件，请填写 name=value")
			}
			r.Headers = append(r.Headers, Pair{"Cookie", v})
		case "-A", "--user-agent", "-e", "--referer":
			var v string
			v, err = take()
			k := "User-Agent"
			if name == "-e" || name == "--referer" {
				k = "Referer"
			}
			r.Headers = append(r.Headers, Pair{k, v})
		case "--url":
			if r.URL != "" {
				err = fmt.Errorf("一次只支持一个 URL")
			} else {
				r.URL, err = take()
			}
		case "-m", "--max-time":
			var v string
			v, err = take()
			if err == nil {
				var n float64
				n, err = strconv.ParseFloat(v, 64)
				if err == nil {
					if math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 3600 || math.Trunc(n) != n {
						err = fmt.Errorf("超时请填写 1–3600 的整数秒")
					} else {
						r.TimeoutSeconds = int(n)
					}
				}
			}
		case "-L", "--location":
			if attached {
				return r, fmt.Errorf("%s 不接受参数", name)
			}
			r.FollowRedirects = true
		case "-I", "--head":
			if attached {
				return r, fmt.Errorf("%s 不接受参数", name)
			}
			r.Method = "HEAD"
			explicitMethod = true
		case "--compressed", "--silent", "--show-error", "-s", "-S", "-sS", "-i", "--include", "-N", "--no-buffer":
			if attached {
				err = fmt.Errorf("%s 不接受参数", name)
			}
		default:
			if strings.HasPrefix(arg, "-") {
				err = fmt.Errorf("暂不支持 curl 参数 %s；未导入，避免改变请求含义", name)
			} else if r.URL != "" {
				err = fmt.Errorf("一次只支持一个 URL")
			} else {
				r.URL = arg
			}
		}
		if err != nil {
			return r, err
		}
	}
	if r.URL == "" {
		return r, fmt.Errorf("curl 缺少 URL")
	}
	if (hasBody || r.BodyMode == BodyMultipart) && !explicitMethod {
		r.Method = "POST"
	}
	r.Body = strings.Join(data, "&")
	if jsonBody {
		r.Body = strings.Join(data, "")
	}
	addDefault := func(k, v string) {
		for _, p := range r.Headers {
			if strings.EqualFold(p.Key, k) {
				return
			}
		}
		r.Headers = append(r.Headers, Pair{k, v})
	}
	if hasBody {
		if jsonBody {
			addDefault("Content-Type", "application/json")
			addDefault("Accept", "application/json")
		} else {
			addDefault("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if u, e := url.Parse(r.URL); e == nil && u.User != nil {
		pass, _ := u.User.Password()
		r.Headers = append(r.Headers, Pair{"Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pass))})
		u.User = nil
		r.URL = u.String()
	}
	r.Name = "导入的请求"
	return r, nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func ExportCURL(r Request, s Settings) (string, error) {
	if r.Protocol == "WebSocket" {
		return "", fmt.Errorf("curl 导出仅用于 HTTP 和 SSE 请求")
	}
	r, err := Resolve(r, s)
	if err != nil {
		return "", err
	}
	parts := []string{"curl", "--request", shellQuote(r.Method), "--url", shellQuote(r.URL), "--max-time", strconv.Itoa(r.TimeoutSeconds)}
	if r.FollowRedirects {
		parts = append(parts, "--location")
	}
	if r.Protocol == "SSE" {
		parts = append(parts, "--no-buffer")
		hasAccept := false
		for _, h := range r.Headers {
			if strings.EqualFold(h.Key, "Accept") {
				hasAccept = true
			}
		}
		if !hasAccept {
			parts = append(parts, "--header", shellQuote("Accept: text/event-stream"))
		}
	}
	if r.Method == "HEAD" {
		parts = append(parts, "--head")
	}
	for _, h := range r.Headers {
		parts = append(parts, "--header", shellQuote(h.Key+": "+h.Value))
	}
	if r.BodyMode == BodyMultipart {
		if len(r.FormParts) == 0 {
			return "", fmt.Errorf("请先添加表单字段再复制 curl")
		}
		for _, p := range r.FormParts {
			if strings.ContainsRune(p.Name, '=') {
				return "", fmt.Errorf("curl 不支持包含等号的字段名称")
			}
			if p.Kind == FormText {
				parts = append(parts, "--form-string", shellQuote(p.Name+"="+p.Value))
			} else {
				path := strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(p.FilePath)
				parts = append(parts, "--form", shellQuote(p.Name+"=@\""+path+"\""))
			}
		}
		return strings.Join(parts, " "), nil
	}
	if r.Body != "" {
		hasType := false
		for _, h := range r.Headers {
			if strings.EqualFold(h.Key, "Content-Type") {
				hasType = true
			}
		}
		if !hasType {
			parts = append(parts, "--header", shellQuote("Content-Type: application/octet-stream"))
		}
		parts = append(parts, "--data-raw", shellQuote(r.Body))
	}
	return strings.Join(parts, " "), nil
}
