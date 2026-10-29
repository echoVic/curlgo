package core

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Bruno stores each request as a .bru file. The format is documented at
// https://docs.usebruno.com/bru-lang/overview; parsing follows the grammar in
// packages/bruno-lang/v2/src/bruToJson.js.

type bruPair struct {
	key, value string
	enabled    bool
}

type bruBlock struct {
	name  string
	pairs []bruPair // dictionary blocks
	text  string    // text blocks, outdented by two spaces
}

// brunoTextBlocks hold free text up to a "}" at the start of a line.
var brunoTextBlocks = map[string]bool{
	"body": true, "body:json": true, "body:text": true, "body:xml": true, "body:sparql": true,
	"body:graphql": true, "body:graphql:vars": true, "script:pre-request": true,
	"script:post-response": true, "tests": true, "docs": true, "example": true,
}

var brunoMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
	"options": true, "head": true, "connect": true, "trace": true,
}

func parseBru(source string) ([]bruBlock, error) {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var blocks []bruBlock
	for i := 0; i < len(lines); i++ {
		header := strings.TrimSpace(lines[i])
		if header == "" {
			continue
		}
		name, rest, _ := strings.Cut(header, " ")
		rest = strings.TrimSpace(rest)
		if strings.HasSuffix(name, "{") && rest == "" {
			name, rest = strings.TrimSuffix(name, "{"), "{"
		}
		if name == "vars:secret" {
			// A list such as "vars:secret [ a, b ]"; the names carry no values.
			block := bruBlock{name: name}
			for ; i < len(lines) && !strings.Contains(lines[i], "]"); i++ {
				block.text += lines[i] + "\n"
			}
			if i == len(lines) {
				return nil, fmt.Errorf("vars:secret 缺少结尾的 ]")
			}
			blocks = append(blocks, block)
			continue
		}
		if rest != "{" {
			return nil, fmt.Errorf("第 %d 行无法识别：%s", i+1, header)
		}
		block := bruBlock{name: name}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "}") {
			end++
		}
		if end == len(lines) {
			return nil, fmt.Errorf("%s 块缺少结尾的 }", name)
		}
		body := lines[i+1 : end]
		if brunoTextBlocks[name] {
			for j, line := range body {
				body[j] = strings.TrimPrefix(line, "  ")
			}
			block.text = strings.Join(body, "\n")
		} else {
			var err error
			if block.pairs, err = parseBruPairs(body); err != nil {
				return nil, fmt.Errorf("%s 块：%w", name, err)
			}
		}
		blocks = append(blocks, block)
		i = end
	}
	return blocks, nil
}

func parseBruPairs(lines []string) ([]bruPair, error) {
	var pairs []bruPair
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if isBruAnnotation(line) {
			// @description(...) and similar annotations document the next pair.
			if strings.Count(line, "'''") == 1 {
				for i++; i < len(lines) && !strings.Contains(lines[i], "'''"); i++ {
				}
			}
			continue
		}
		p := bruPair{enabled: true}
		if strings.HasPrefix(line, "~") {
			p.enabled = false
			line = line[1:]
		}
		var rest string
		if strings.HasPrefix(line, `"`) {
			var b strings.Builder
			j := 1
			for ; j < len(line) && line[j] != '"'; j++ {
				if line[j] == '\\' && j+1 < len(line) && line[j+1] == '"' {
					j++
				}
				b.WriteByte(line[j])
			}
			if j == len(line) {
				return nil, fmt.Errorf("引号未闭合：%s", line)
			}
			p.key = b.String()
			rest = strings.TrimLeft(line[j+1:], " \t")
			if !strings.HasPrefix(rest, ":") {
				return nil, fmt.Errorf("缺少冒号：%s", line)
			}
			rest = rest[1:]
		} else {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("缺少冒号：%s", line)
			}
			p.key, rest = strings.TrimSpace(key), value
		}
		rest = strings.TrimSpace(rest)
		if rest == "[" {
			// A list value such as meta tags; CurlGo has no use for its items.
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "]"; i++ {
			}
			if i == len(lines) {
				return nil, fmt.Errorf("%s 的列表缺少结尾的 ]", p.key)
			}
			rest = ""
		}
		if strings.HasPrefix(rest, "'''") {
			// A multi-line value; content lines are indented four spaces.
			var text []string
			closed := false
			for i++; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "'''") {
					closed = true
					break
				}
				if len(lines[i]) >= 4 {
					text = append(text, lines[i][4:])
				} else {
					text = append(text, "")
				}
			}
			if !closed {
				return nil, fmt.Errorf("%s 的多行值缺少结尾的 '''", p.key)
			}
			rest = strings.Join(text, "\n")
		}
		p.value = rest
		pairs = append(pairs, p)
	}
	return pairs, nil
}

// isBruAnnotation matches "@name" or "@name(...)" on its own line. A line such
// as "@key: value" is an ordinary pair whose key starts with @.
func isBruAnnotation(line string) bool {
	name, ok := strings.CutPrefix(line, "@")
	if !ok {
		return false
	}
	end := strings.IndexAny(name, "( \t:")
	if end < 0 {
		return name != ""
	}
	return end > 0 && name[end] == '('
}

func findBlock(blocks []bruBlock, name string) *bruBlock {
	for i := range blocks {
		if blocks[i].name == name {
			return &blocks[i]
		}
	}
	return nil
}

func (b *bruBlock) get(key string) string {
	if b == nil {
		return ""
	}
	for _, p := range b.pairs {
		if p.key == key {
			return p.value
		}
	}
	return ""
}

func (b *bruBlock) enabledPairs() []bruPair {
	if b == nil {
		return nil
	}
	var out []bruPair
	for _, p := range b.pairs {
		if p.enabled {
			out = append(out, p)
		}
	}
	return out
}

type brunoAuth struct {
	mode   string
	fields map[string]string
}

// readBrunoAuth reads the auth:<mode> block for mode.
func readBrunoAuth(blocks []bruBlock, mode string) brunoAuth {
	a := brunoAuth{mode: mode, fields: map[string]string{}}
	if b := findBlock(blocks, "auth:"+mode); b != nil {
		for _, p := range b.pairs {
			a.fields[p.key] = p.value
		}
	}
	return a
}

func (a brunoAuth) apply(r *Request) error {
	switch a.mode {
	case "", "none":
		r.Auth = Authentication{}
	case "bearer":
		r.Auth = Authentication{Mode: AuthBearer, Token: a.fields["token"]}
	case "basic":
		r.Auth = Authentication{Mode: AuthBasic, Username: a.fields["username"], Password: a.fields["password"]}
	case "apikey":
		location := APIKeyLocation(a.fields["placement"])
		switch location {
		case "", "headers":
			location = APIKeyHeader
		case "queryparams":
			location = APIKeyQuery
		}
		r.Auth = Authentication{Mode: AuthAPIKey, Key: a.fields["key"], Value: a.fields["value"], Location: location}
	default:
		return fmt.Errorf("暂不支持 %s 认证", a.mode)
	}
	return nil
}

// brunoContext carries what a request inherits from collection.bru and its
// folders' folder.bru files.
type brunoContext struct {
	root    string // collection directory; relative @file paths resolve here
	prefix  string
	groupID string
	headers []Pair
	auth    brunoAuth
}

// mergeHeaders overrides inherited headers with same-named ones, like Bruno.
func mergeHeaders(base []Pair, override []Pair) []Pair {
	out := append([]Pair(nil), base...)
	for _, h := range override {
		replaced := false
		for i := range out {
			if strings.EqualFold(out[i].Key, h.Key) {
				out[i].Value = h.Value
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, h)
		}
	}
	return out
}

func brunoHeaders(blocks []bruBlock) []Pair {
	var out []Pair
	for _, p := range findBlock(blocks, "headers").enabledPairs() {
		out = append(out, Pair{p.key, p.value})
	}
	return out
}

// parseBrunoRequest converts one request .bru file. Unsupported features return
// an error so the request is skipped rather than imported with a changed meaning.
func parseBrunoRequest(source string, ctx brunoContext) (Request, []string, error) {
	var warnings []string
	r := NewRequest()
	blocks, err := parseBru(source)
	if err != nil {
		return r, nil, err
	}
	meta := findBlock(blocks, "meta")
	if kind := meta.get("type"); kind != "" && kind != "http" && kind != "graphql" {
		return r, nil, fmt.Errorf("暂不支持 %s 类型的请求", kind)
	}
	var http *bruBlock
	for i := range blocks {
		if brunoMethods[blocks[i].name] || blocks[i].name == "http" {
			if http != nil {
				return r, nil, fmt.Errorf("包含多个请求方法")
			}
			http = &blocks[i]
		}
	}
	if http == nil {
		return r, nil, fmt.Errorf("没有找到请求方法")
	}
	r.Method = strings.ToUpper(http.name)
	if http.name == "http" {
		r.Method = strings.ToUpper(http.get("method"))
	}
	r.Name = strings.TrimSpace(meta.get("name"))
	r.GroupID = ctx.groupID
	path := map[string]string{}
	for _, p := range findBlock(blocks, "params:path").enabledPairs() {
		path[p.key] = p.value
	}
	// Enabled params:query entries are already part of the url line.
	r.URL = substitutePathParams(strings.TrimSpace(http.get("url")), path)
	r.Headers = mergeHeaders(ctx.headers, brunoHeaders(blocks))

	switch mode := http.get("body"); mode {
	case "", "none":
	case "json", "text", "xml":
		if b := findBlock(blocks, "body:"+mode); b != nil {
			r.Body = b.text
		} else if b := findBlock(blocks, "body"); b != nil && mode == "json" {
			r.Body = b.text
		}
		r.Headers = withDefaultContentType(r.Headers, defaultContentTypes[mode])
	case "formUrlEncoded":
		var fields []formField
		for _, p := range findBlock(blocks, "body:form-urlencoded").enabledPairs() {
			fields = append(fields, formField{p.key, p.value})
		}
		body, hasVariable := encodeForm(fields)
		if hasVariable {
			warnings = append(warnings, "表单包含变量，发送时变量值不会再次编码，值含 & = + 等字符时请手动编码")
		}
		r.Body = body
		r.Headers = withDefaultContentType(r.Headers, defaultContentTypes["form"])
	case "multipartForm":
		r.BodyMode = BodyMultipart
		for _, p := range findBlock(blocks, "body:multipart-form").enabledPairs() {
			value := p.value
			if strings.Contains(value, "@contentType(") {
				return r, nil, fmt.Errorf("字段 %s 指定了 Content-Type，暂不支持", p.key)
			}
			if file, ok := strings.CutPrefix(value, "@file("); ok && strings.HasSuffix(file, ")") {
				file = strings.TrimSuffix(file, ")")
				if file == "" || strings.Contains(file, "|") {
					return r, nil, fmt.Errorf("文件字段 %s 未选择单个文件", p.key)
				}
				if !filepath.IsAbs(file) {
					if ctx.root == "" {
						return r, nil, fmt.Errorf("文件字段 %s 使用相对路径，请导入整个集合文件夹", p.key)
					}
					file = filepath.Join(ctx.root, file)
				}
				warnings = append(warnings, fmt.Sprintf("引用本地文件 %s，发送时才会读取，请确认文件存在", file))
				r.FormParts = append(r.FormParts, FormPart{ID: NewID(), Kind: FormFile, Name: p.key, FilePath: file})
			} else {
				r.FormParts = append(r.FormParts, FormPart{ID: NewID(), Kind: FormText, Name: p.key, Value: value})
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
		vars := ""
		if b := findBlock(blocks, "body:graphql:vars"); b != nil {
			vars = b.text
		}
		query := ""
		if b := findBlock(blocks, "body:graphql"); b != nil {
			query = b.text
		}
		if r.Body, err = graphQLBody(query, vars); err != nil {
			return r, nil, err
		}
		r.Headers = withDefaultContentType(r.Headers, defaultContentTypes["json"])
	case "file":
		return r, nil, fmt.Errorf("二进制文件正文暂不支持")
	default:
		return r, nil, fmt.Errorf("暂不支持 %s 正文", mode)
	}

	auth := http.get("auth")
	if auth == "" {
		auth = "none"
	}
	effective := readBrunoAuth(blocks, auth)
	if auth == "inherit" {
		effective = ctx.auth
	}
	if err := effective.apply(&r); err != nil {
		return r, nil, err
	}

	if settings := findBlock(blocks, "settings"); settings != nil {
		if v := settings.get("followRedirects"); v != "" {
			r.FollowRedirects = v == "true"
		}
		if v := settings.get("timeout"); v != "" && v != "inherit" && v != "0" {
			ms, err := strconv.Atoi(v)
			if err != nil || ms < 1 || ms > 3600*1000 {
				return r, nil, fmt.Errorf("超时设置 %s 毫秒超出 1–3600 秒", v)
			}
			r.TimeoutSeconds = max(1, (ms+999)/1000)
		}
	}
	for _, name := range []string{"script:pre-request", "script:post-response", "tests"} {
		if b := findBlock(blocks, name); b != nil && strings.TrimSpace(b.text) != "" {
			warnings = append(warnings, "包含脚本或测试，CurlGo 不会执行")
			break
		}
	}
	if len(findBlock(blocks, "vars:pre-request").enabledPairs()) > 0 || len(findBlock(blocks, "vars:post-response").enabledPairs()) > 0 {
		warnings = append(warnings, "定义了请求变量，未导入，请在环境中设置")
	}
	return r, warnings, checkImported(r)
}

const maxBrunoFiles = 5000

// readBounded reads a regular file up to MaxInput bytes without following symlinks.
func readBounded(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("不是普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxInput+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxInput {
		return "", fmt.Errorf("文件超过 1 MiB")
	}
	return string(data), nil
}

type brunoEntry struct {
	seq  int
	name string
	file string
}

// ImportBrunoDirectory reads a Bruno collection folder (the one containing
// bruno.json). Symlinks are never followed, and nothing outside the folder is read.
func ImportBrunoDirectory(root string) (CollectionImport, error) {
	out := CollectionImport{Environments: map[string]map[string]string{}}
	config, err := readBounded(filepath.Join(root, "bruno.json"))
	if err != nil {
		return out, fmt.Errorf("请选择包含 bruno.json 的 Bruno 集合文件夹")
	}
	var meta struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(config), &meta) != nil {
		return out, fmt.Errorf("bruno.json 不是有效的 JSON")
	}
	out.Name = strings.TrimSpace(meta.Name)
	if out.Name == "" {
		out.Name = filepath.Base(root)
	}

	out.RootGroupID = NewID()
	out.Groups = append(out.Groups, RequestGroup{ID: out.RootGroupID, Name: out.Name})
	ctx := brunoContext{root: root, prefix: out.Name, groupID: out.RootGroupID, auth: brunoAuth{mode: "none"}}
	collectionVars := map[string]string{}
	if source, err := readBounded(filepath.Join(root, "collection.bru")); err == nil {
		blocks, err := parseBru(source)
		if err != nil {
			return out, fmt.Errorf("collection.bru：%w", err)
		}
		ctx = applyBrunoFolder(&out, ctx, blocks, "collection.bru")
		for _, p := range findBlock(blocks, "vars:pre-request").enabledPairs() {
			collectionVars[p.key] = p.value
		}
	}

	if entries, err := os.ReadDir(filepath.Join(root, "environments")); err == nil {
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".bru")
			if !ok || !e.Type().IsRegular() {
				continue
			}
			source, err := readBounded(filepath.Join(root, "environments", e.Name()))
			if err == nil {
				var blocks []bruBlock
				if blocks, err = parseBru(source); err == nil {
					vars := map[string]string{}
					for k, v := range collectionVars {
						vars[k] = v
					}
					for _, p := range findBlock(blocks, "vars").enabledPairs() {
						vars[p.key] = p.value
					}
					out.Environments[name] = vars
					if secret := findBlock(blocks, "vars:secret"); secret != nil {
						names := strings.Fields(strings.NewReplacer("vars:secret", " ", "[", " ", "]", " ", ",", " ").Replace(secret.text))
						if len(names) > 0 {
							out.warn("环境「%s」的密钥变量 %s 未保存在文件中，请手动填写", name, strings.Join(names, "、"))
						}
					}
					continue
				}
			}
			out.warn("已跳过环境 %s：%v", e.Name(), err)
		}
	}
	if len(out.Environments) == 0 && len(collectionVars) > 0 {
		out.Environments[out.Name] = collectionVars
	}

	files := 0
	var walk func(dir string, ctx brunoContext, depth int)
	walk = func(dir string, ctx brunoContext, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			out.warn("无法读取文件夹 %s：%v", dir, err)
			return
		}
		var requests, folders []brunoEntry
		for _, e := range entries {
			rel, _ := filepath.Rel(root, filepath.Join(dir, e.Name()))
			if e.Type()&fs.ModeSymlink != 0 {
				out.warn("已跳过符号链接 %s", rel)
				continue
			}
			if files++; files > maxBrunoFiles {
				if files == maxBrunoFiles+1 {
					out.warn("文件超过 %d 个，其余文件未读取", maxBrunoFiles)
				}
				return
			}
			if e.IsDir() {
				if depth == 0 && (e.Name() == "environments" || e.Name() == "node_modules") || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				folders = append(folders, brunoEntry{name: e.Name(), file: filepath.Join(dir, e.Name())})
				continue
			}
			if !strings.HasSuffix(e.Name(), ".bru") || e.Name() == "folder.bru" || depth == 0 && e.Name() == "collection.bru" {
				continue
			}
			requests = append(requests, brunoEntry{name: e.Name(), file: filepath.Join(dir, e.Name())})
		}
		type parsed struct {
			brunoEntry
			request  Request
			warnings []string
		}
		var ready []parsed
		for _, entry := range requests {
			rel, _ := filepath.Rel(root, entry.file)
			source, err := readBounded(entry.file)
			var r Request
			var warnings []string
			if err == nil {
				r, warnings, err = parseBrunoRequest(source, ctx)
			}
			if err != nil {
				out.warn("已跳过 %s：%v", rel, err)
				continue
			}
			if r.Name == "" {
				r.Name = strings.TrimSuffix(entry.name, ".bru")
			}
			seq := 1 << 30
			if blocks, err := parseBru(source); err == nil {
				if n, err := strconv.Atoi(findBlock(blocks, "meta").get("seq")); err == nil {
					seq = n
				}
			}
			entry.seq = seq
			ready = append(ready, parsed{entry, r, warnings})
		}
		sort.SliceStable(ready, func(i, k int) bool {
			if ready[i].seq != ready[k].seq {
				return ready[i].seq < ready[k].seq
			}
			return ready[i].name < ready[k].name
		})
		for _, p := range ready {
			if len(out.Requests) >= MaxImportRequests {
				out.warn("请求超过 %d 个，其余请求未导入", MaxImportRequests)
				return
			}
			for _, w := range p.warnings {
				out.warn("「%s / %s」%s", ctx.prefix, p.request.Name, w)
			}
			out.Requests = append(out.Requests, p.request)
		}
		if depth >= 16 {
			if len(folders) > 0 {
				out.warn("%s 下的文件夹层级过深，已跳过", dir)
			}
			return
		}
		sort.Slice(folders, func(i, k int) bool { return folders[i].name < folders[k].name })
		for _, f := range folders {
			if len(out.Groups) >= MaxGroups {
				out.warn("分组超过 %d 个，其余文件夹未导入", MaxGroups)
				return
			}
			child := ctx
			name := f.name
			if source, err := readBounded(filepath.Join(f.file, "folder.bru")); err == nil {
				if blocks, err := parseBru(source); err == nil {
					if n := strings.TrimSpace(findBlock(blocks, "meta").get("name")); n != "" {
						name = n
					}
					child = applyBrunoFolder(&out, child, blocks, name)
				} else {
					out.warn("已忽略 %s/folder.bru：%v", f.name, err)
				}
			}
			child.prefix = name
			if ctx.prefix != "" {
				child.prefix = ctx.prefix + " / " + name
			}
			child.groupID = NewID()
			out.Groups = append(out.Groups, RequestGroup{ID: child.groupID, Name: name, ParentID: ctx.groupID})
			walk(f.file, child, depth+1)
		}
	}
	walk(root, ctx, 0)
	return out, ValidateRequestGroups(out.Groups)
}

// applyBrunoFolder layers collection.bru or folder.bru settings onto ctx.
func applyBrunoFolder(out *CollectionImport, ctx brunoContext, blocks []bruBlock, name string) brunoContext {
	ctx.headers = mergeHeaders(ctx.headers, brunoHeaders(blocks))
	if mode := findBlock(blocks, "auth").get("mode"); mode != "" && mode != "inherit" {
		ctx.auth = readBrunoAuth(blocks, mode)
	}
	for _, script := range []string{"script:pre-request", "script:post-response", "tests"} {
		if b := findBlock(blocks, script); b != nil && strings.TrimSpace(b.text) != "" {
			out.warn("「%s」包含脚本，CurlGo 不会执行", name)
			break
		}
	}
	return ctx
}
