package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type saveJob struct {
	workspace core.Workspace
	revision  uint64
	final     bool
}
type application struct {
	win                                                                    *mygo.Window
	workspace                                                              core.Workspace
	engine                                                                 *core.Engine
	store                                                                  *core.Store
	updates                                                                func(func())
	closeNative                                                            func()
	saves                                                                  chan saveJob
	revision                                                               uint64
	closing, closed, saveReady, readOnly                                   bool
	page, requestTab, responseTab, libraryTab                              int
	headers, globalHeaders, variables, envName, search, curlInput, message string
	timeout                                                                string
	status, notice, saveStatus                                             string
	response, stream                                                       string
	responseHeaders                                                        http.Header
	responseEditor                                                         *editor.State
	responseVersion                                                        string
	busy                                                                   bool
	cancel                                                                 context.CancelFunc
	socket                                                                 *core.Socket
	generation                                                             uint64
	split                                                                  float32
	urlHandle                                                              ui.Handle
	list                                                                   ui.ListState
	toast                                                                  string
	caCertPEM                                                              string
	caError                                                                error
	importingCA                                                            bool
	selectingFile                                                          string
	pickUploadFile                                                         func(mygo.OpenDialogOptions) ([]string, error)
}

func newApplication(w core.Workspace) *application {
	a := &application{workspace: w, engine: core.NewEngine(), split: 240, status: "准备就绪", saveStatus: "所有内容仅保存在本机"}
	a.pickUploadFile = mygo.Dialog.Open
	a.engine.RestoreCookies(w.Cookies)
	a.engine.OnCookiesChanged(func() {
		a.post(func() { a.persist() })
	})
	a.syncDraft()
	a.syncSettings()
	a.caError = a.engine.SetCACertificates(w.Settings.CACertPEM)
	if a.caError != nil {
		a.notice = a.caError.Error()
	}
	a.responseEditor = &editor.State{Buffer: editor.NewBuffer(""), ReadOnly: true, SoftWrap: true}
	a.responseEditor.Buffer.SetLanguage(editor.LanguageNamed("JSON"))
	return a
}
func (a *application) post(fn func()) {
	if a.updates != nil {
		a.updates(fn)
	}
}
func cloneWorkspace(w core.Workspace) core.Workspace {
	w.Cookies = append([]core.SavedCookie(nil), w.Cookies...)
	for i := range w.Cookies {
		w.Cookies[i].Cookie.Unparsed = append([]string(nil), w.Cookies[i].Cookie.Unparsed...)
	}
	w.Draft.Headers = append([]core.Pair(nil), w.Draft.Headers...)
	w.Draft.FormParts = append([]core.FormPart(nil), w.Draft.FormParts...)
	w.Requests = append([]core.Request(nil), w.Requests...)
	for i := range w.Requests {
		w.Requests[i].Headers = append([]core.Pair(nil), w.Requests[i].Headers...)
		w.Requests[i].FormParts = append([]core.FormPart(nil), w.Requests[i].FormParts...)
	}
	w.History = append([]core.History(nil), w.History...)
	for i := range w.History {
		w.History[i].Request.Headers = append([]core.Pair(nil), w.History[i].Request.Headers...)
		w.History[i].Request.FormParts = append([]core.FormPart(nil), w.History[i].Request.FormParts...)
	}
	w.Settings.GlobalHeaders = append([]core.Pair(nil), w.Settings.GlobalHeaders...)
	envs := make(map[string]map[string]string, len(w.Settings.Environments))
	for name, vars := range w.Settings.Environments {
		cp := make(map[string]string, len(vars))
		for k, v := range vars {
			cp[k] = v
		}
		envs[name] = cp
	}
	w.Settings.Environments = envs
	return w
}
func (a *application) startSaving() {
	a.saves = make(chan saveJob, 1)
	store := a.store
	post := a.updates
	go func() {
		for job := range a.saves {
			if !job.final {
				timer := time.NewTimer(250 * time.Millisecond)
			collecting:
				for {
					select {
					case newer, ok := <-a.saves:
						if !ok {
							timer.Stop()
							return
						}
						job = newer
						if job.final {
							break collecting
						}
					case <-timer.C:
						break collecting
					}
				}
				timer.Stop()
			}
			err := store.Save(job.workspace)
			post(func() {
				if a.closed {
					return
				}
				if err != nil {
					a.saveStatus = "保存失败：" + err.Error()
					if job.final {
						a.closing = false
					}
					a.notice = a.saveStatus
				} else if job.revision == a.revision {
					a.saveStatus = "已保存到本机"
				}
				if job.final && err == nil {
					a.saveReady = true
					a.closeNative()
				}
			})
		}
	}()
}
func (a *application) persist() {
	if a.readOnly || a.closing || a.closed {
		return
	}
	a.revision++
	a.saveStatus = "正在保存…"
	a.workspace.Cookies = a.engine.CookieSnapshot()
	if a.saves == nil {
		return
	}
	job := saveJob{workspace: cloneWorkspace(a.workspace), revision: a.revision}
	select {
	case a.saves <- job:
	default:
		select {
		case <-a.saves:
		default:
		}
		a.saves <- job
	}
}
func (a *application) closeWindow(e *mygo.CloseEvent) {
	if a.saveReady || a.readOnly {
		return
	}
	e.PreventDefault()
	if a.closing {
		return
	}
	a.stop()
	a.workspace.Cookies = a.engine.CookieSnapshot()
	a.closing = true
	job := saveJob{workspace: cloneWorkspace(a.workspace), revision: a.revision, final: true}
	select {
	case <-a.saves:
	default:
	}
	a.saves <- job
}
func (a *application) syncDraft() {
	for i := range a.workspace.Draft.FormParts {
		if a.workspace.Draft.FormParts[i].ID == "" {
			a.workspace.Draft.FormParts[i].ID = core.NewID()
		}
	}
	a.headers = core.FormatHeaders(a.workspace.Draft.Headers)
	a.timeout = fmt.Sprint(a.workspace.Draft.TimeoutSeconds)
}
func (a *application) syncSettings() {
	a.globalHeaders = core.FormatHeaders(a.workspace.Settings.GlobalHeaders)
	a.caCertPEM = a.workspace.Settings.CACertPEM
	a.envName = a.workspace.Settings.Environment
	b, _ := json.MarshalIndent(a.workspace.Settings.Environments[a.envName], "", "  ")
	a.variables = string(b)
}
func (a *application) selectRequest(r core.Request) {
	a.stop()
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	a.workspace.Draft = r
	a.syncDraft()
	a.response = ""
	a.stream = ""
	a.responseHeaders = nil
	a.status = "准备就绪"
	a.page = 0
	a.notice = ""
	a.persist()
}
func (a *application) newRequest() { a.selectRequest(core.NewRequest()) }
func (a *application) saveRequest() {
	if !a.validateDraft() {
		return
	}
	r := a.workspace.Draft
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	if strings.TrimSpace(r.Name) == "" {
		a.notice = "先给请求起一个名字"
		return
	}
	for i, v := range a.workspace.Requests {
		if v.ID == r.ID {
			a.workspace.Requests[i] = r
			a.toast = "请求已更新"
			a.persist()
			return
		}
	}
	a.workspace.Requests = append(a.workspace.Requests, r)
	a.toast = "请求已收藏"
	a.persist()
}
func (a *application) deleteRequest(id string) {
	for i, r := range a.workspace.Requests {
		if r.ID == id {
			a.workspace.Requests = append(a.workspace.Requests[:i], a.workspace.Requests[i+1:]...)
			break
		}
	}
	a.persist()
}
func (a *application) importCURL() {
	r, err := core.ImportCURL(a.curlInput)
	if err != nil {
		a.notice = err.Error()
		return
	}
	a.selectRequest(r)
	a.curlInput = ""
	a.toast = "已导入，可以检查后发送"
}
func (a *application) applySettings() {
	h, err := core.ParseHeaders(a.globalHeaders)
	if err != nil {
		a.notice = err.Error()
		return
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal([]byte(a.variables), &raw); err != nil || raw == nil {
		a.notice = "环境变量应为 JSON 对象，值均为字符串"
		return
	}
	vars := make(map[string]string, len(raw))
	for k, v := range raw {
		var text string
		if len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &text) != nil {
			a.notice = "环境变量应为 JSON 对象，值均为字符串"
			return
		}
		vars[k] = text
	}
	if vars == nil {
		a.notice = "环境变量应为 JSON 对象，值均为字符串"
		return
	}
	name := strings.TrimSpace(a.envName)
	if name == "" {
		a.notice = "请填写环境名称"
		return
	}
	if err := a.engine.SetCACertificates(a.caCertPEM); err != nil {
		a.notice = err.Error()
		return
	}
	a.caError = nil
	a.notice = ""
	a.workspace.Settings.GlobalHeaders = h
	a.workspace.Settings.CACertPEM = a.caCertPEM
	a.workspace.Settings.Environments[name] = vars
	a.workspace.Settings.Environment = name
	a.toast = "环境与全局请求头已保存"
	a.persist()
}
func readCACertificates(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("无法读取 CA 证书：%w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, core.MaxInput+1))
	if err != nil {
		return "", fmt.Errorf("无法读取 CA 证书：%w", err)
	}
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("CA 证书文件为空")
	}
	if _, err := core.ParseCACertificates(text); err != nil {
		return "", err
	}
	return text, nil
}
func (a *application) chooseCACertificates() {
	if a.importingCA {
		return
	}
	a.importingCA = true
	parent := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: parent, Title: "导入 CA 证书", ButtonLabel: "导入",
			Message: "选择 PEM 格式的公开证书，可包含多张证书。",
			Filters: []mygo.FileFilter{{Name: "PEM 证书", Extensions: []string{"pem", "crt", "cer"}}},
		})
		var text string
		if err == nil && len(paths) > 0 {
			text, err = readCACertificates(paths[0])
		}
		a.post(func() {
			a.importingCA = false
			if a.closed || a.closing {
				return
			}
			if err != nil {
				a.notice = err.Error()
			} else if len(paths) > 0 {
				a.caCertPEM = text
				a.notice = ""
				a.toast = "证书已导入，保存配置后生效"
			}
		})
	}()
}
func (a *application) stop() {
	a.generation++
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.socket != nil {
		socket := a.socket
		go socket.Close()
		a.socket = nil
	}
	if a.busy {
		a.status = "已停止"
	}
	a.busy = false
}
func (a *application) addHistory(r core.Request, status string) {
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	a.workspace.History = append([]core.History{{Request: r, At: time.Now(), Status: status}}, a.workspace.History...)
	if len(a.workspace.History) > 100 {
		a.workspace.History = a.workspace.History[:100]
	}
	a.persist()
}
func (a *application) send() {
	if a.busy {
		a.stop()
		return
	}
	if a.closing {
		return
	}
	if a.caError != nil {
		a.notice = a.caError.Error() + "；请在设置中修正或清除证书"
		return
	}
	if !a.validateDraft() {
		return
	}
	r, err := core.Resolve(a.workspace.Draft, a.workspace.Settings)
	if err != nil {
		a.notice = err.Error()
		return
	}
	a.notice = ""
	a.response = ""
	a.stream = ""
	a.responseHeaders = nil
	a.status = "连接中…"
	a.busy = true
	a.generation++
	generation := a.generation
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.addHistory(a.workspace.Draft, "已发送")
	engine := a.engine
	post := a.updates
	// A bounded queue applies backpressure; UI updates are batched at 30 Hz.
	events := make(chan core.Event, 64)
	emit := func(e core.Event) {
		select {
		case events <- e:
		case <-ctx.Done():
		}
	}
	go func() {
		var runErr error
		if r.Protocol == "WebSocket" {
			var socket *core.Socket
			var event core.Event
			socket, event, runErr = engine.Connect(ctx, r)
			if event.Status != "" {
				emit(event)
			}
			if runErr == nil {
				post(func() {
					if a.closed || a.generation != generation {
						socket.Close()
						return
					}
					a.socket = socket
					a.status = "WebSocket 已连接"
				})
				runErr = socket.Read(ctx, emit)
				socket.Close()
			}
		} else {
			runErr = engine.Run(ctx, r, emit)
		}
		if runErr != nil && ctx.Err() == nil {
			emit(core.Event{Kind: "error", Text: runErr.Error()})
		}
		close(events)
	}()
	go func() {
		ticker := time.NewTicker(time.Second / 30)
		defer ticker.Stop()
		defer cancel()
		var pending []core.Event
		flush := func(done bool) bool {
			batch := pending
			pending = nil
			ack := make(chan struct{})
			post(func() {
				defer close(ack)
				if a.closed || a.generation != generation {
					return
				}
				for _, e := range batch {
					a.applyEvent(e)
				}
				if done {
					a.busy = false
					a.socket = nil
					a.cancel = nil
					if r.Protocol != "HTTP" {
						a.status += " · 已结束"
					}
				}
			})
			select {
			case <-ack:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case e, ok := <-events:
				if !ok {
					flush(true)
					return
				}
				pending = append(pending, e)
				if len(pending) >= 64 {
					if !flush(false) {
						return
					}
				}
			case <-ticker.C:
				if len(pending) > 0 {
					if !flush(false) {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
func (a *application) applyEvent(e core.Event) {
	switch e.Kind {
	case "headers":
		a.responseHeaders = e.Headers
		a.status = fmt.Sprintf("%s  ·  %d ms", e.Status, e.Elapsed.Milliseconds())
	case "body":
		a.response = e.Text
		a.status = fmt.Sprintf("%s  ·  %.1f KiB  ·  %d ms", strings.Split(a.status, "  ·")[0], float64(e.Bytes)/1024, e.Elapsed.Milliseconds())
		if e.Truncated {
			a.notice = "响应超过 4 MiB，当前展示已截断"
		}
	case "sse":
		a.appendStream(fmt.Sprintf("← %s  %s  id=%s\n%s\n", time.Now().Format("15:04:05"), e.Name, e.ID, e.Text))
	case "ws":
		a.appendStream("← " + time.Now().Format("15:04:05") + "\n" + e.Text + "\n")
	case "error":
		a.notice = e.Text
		a.status = "请求失败"
	}
}
func (a *application) appendStream(s string) {
	a.stream += s + "\n"
	if len(a.stream) > 256<<10 {
		a.stream = a.stream[len(a.stream)-(256<<10):]
		if i := strings.IndexByte(a.stream, '\n'); i >= 0 {
			a.stream = a.stream[i+1:]
		}
		a.notice = "流消息仅保留最近 256 KiB"
	}
}
func (a *application) sendMessage() {
	if a.socket == nil {
		return
	}
	text := a.message
	socket := a.socket
	generation := a.generation
	post := a.updates
	go func() {
		err := socket.Send(context.Background(), text)
		post(func() {
			if a.closed || generation != a.generation {
				return
			}
			if err != nil {
				a.notice = err.Error()
				return
			}
			a.appendStream("→ " + time.Now().Format("15:04:05") + "\n" + text + "\n")
			if a.message == text {
				a.message = ""
			}
		})
	}()
}
func (a *application) responseText() string {
	if a.responseTab == 1 {
		var keys []string
		for k := range a.responseHeaders {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			for _, v := range a.responseHeaders[k] {
				fmt.Fprintf(&b, "%s: %s\n", k, v)
			}
		}
		return b.String()
	}
	if a.workspace.Draft.Protocol != "HTTP" {
		return a.stream
	}
	return a.response
}

func (a *application) validateDraft() bool {
	h, err := core.ParseHeaders(a.headers)
	if err != nil {
		a.notice = err.Error()
		return false
	}
	n, err := strconv.Atoi(a.timeout)
	if err != nil || n < 1 || n > 3600 {
		a.notice = "超时为 1–3600 秒"
		return false
	}
	a.workspace.Draft.Headers = h
	a.workspace.Draft.TimeoutSeconds = n
	return true
}
