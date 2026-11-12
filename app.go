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
	"github.com/echoVic/curlgo/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type saveJob struct {
	workspace  core.Workspace
	revision   uint64
	generation uint64
	final      bool
	reply      chan error
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
	saveGeneration                                                         uint64
	closing, closed, saveReady, readOnly                                   bool
	page, requestTab, responseTab, libraryTab                              int
	headers, globalHeaders, variables, envName, search, curlInput, message string
	settingsEnvironment                                                    string // environment the variables editor was loaded from
	timeout                                                                string
	status, notice, saveStatus                                             string
	inputNotice                                                            ownedNotice
	response, stream, responseLanguage                                     string
	responseHeaders                                                        http.Header
	responseEditor                                                         *editor.State
	responseVersion                                                        string
	busy                                                                   bool
	cancel                                                                 context.CancelFunc
	socket                                                                 *core.Socket
	generation                                                             uint64
	split                                                                  float32
	assertionSplit                                                         float32
	urlHandle                                                              ui.Handle
	list                                                                   ui.ListState
	toast                                                                  string
	recordingShortcut, shortcutCapture, shortcutMessage                    string
	caCertPEM                                                              string
	caError                                                                error
	proxyURL, proxyBypass                                                  string
	proxyError                                                             error
	importingCA                                                            bool
	selectingFile                                                          string
	pickUploadFile                                                         func(mygo.OpenDialogOptions) ([]string, error)
	collectionImport                                                       *core.CollectionImport
	collectionList                                                         ui.ListState
	importingCollection                                                    bool
	pickCollectionFile                                                     func(mygo.OpenDialogOptions) ([]string, error)
	queryError                                                             error
	groupFilter, editGroupID, groupName, groupParent                       string
	groupList                                                              ui.ListState
	exportOptions                                                          core.CollectionExportOptions
	exportPreview                                                          collectionExportPreview
	exportingCollection                                                    bool
	pickExportFile                                                         func(mygo.SaveDialogOptions) (string, error)
	showCredentials                                                        bool
	capturedResponse                                                       responseCapture
	extractionNotice                                                       string
	assertionReport                                                        *core.AssertionReport
	assertionNotice                                                        string
	secrets                                                                *vault.Vault
	secretName, secretValue, secretNotice                                  string
	showSecret, secretBusy, secretPageActive                               bool
	secretGeneration                                                       uint64
	secretEditorGeneration                                                 uint64
	secretCancel                                                           context.CancelFunc
	appUpdater                                                             updateBackend
	updatePrefs                                                            updatePreferences
	updateOff                                                              func()
	updateSubscription                                                     uint64
}

func newApplication(w core.Workspace) *application {
	a := &application{workspace: w, engine: core.NewEngine(), split: 240, assertionSplit: 55, status: "准备就绪", saveStatus: "所有内容仅保存在本机"}
	a.pickUploadFile = mygo.Dialog.Open
	a.pickCollectionFile = mygo.Dialog.Open
	a.pickExportFile = mygo.Dialog.Save
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
	a.proxyError = a.engine.SetProxy(w.Settings.Proxy)
	if a.proxyError != nil {
		a.notice = a.proxyError.Error()
	}
	a.responseEditor = &editor.State{Buffer: editor.NewBuffer(""), ReadOnly: true, SoftWrap: true}
	return a
}
func (a *application) post(fn func()) {
	if a.updates != nil {
		a.updates(fn)
	}
}
func cloneWorkspace(w core.Workspace) core.Workspace {
	w.Groups = append([]core.RequestGroup(nil), w.Groups...)
	w.Cookies = append([]core.SavedCookie(nil), w.Cookies...)
	for i := range w.Cookies {
		w.Cookies[i].Cookie.Unparsed = append([]string(nil), w.Cookies[i].Cookie.Unparsed...)
	}
	w.Draft.Headers = append([]core.Pair(nil), w.Draft.Headers...)
	w.Draft.FormParts = append([]core.FormPart(nil), w.Draft.FormParts...)
	w.Draft.QueryParams = append([]core.QueryParam(nil), w.Draft.QueryParams...)
	w.Draft.Extractors = append([]core.ResponseExtractor(nil), w.Draft.Extractors...)
	w.Draft.Assertions = append([]core.ResponseAssertion(nil), w.Draft.Assertions...)
	w.Requests = append([]core.Request(nil), w.Requests...)
	for i := range w.Requests {
		w.Requests[i].Headers = append([]core.Pair(nil), w.Requests[i].Headers...)
		w.Requests[i].FormParts = append([]core.FormPart(nil), w.Requests[i].FormParts...)
		w.Requests[i].QueryParams = append([]core.QueryParam(nil), w.Requests[i].QueryParams...)
		w.Requests[i].Extractors = append([]core.ResponseExtractor(nil), w.Requests[i].Extractors...)
		w.Requests[i].Assertions = append([]core.ResponseAssertion(nil), w.Requests[i].Assertions...)
	}
	w.History = append([]core.History(nil), w.History...)
	for i := range w.History {
		w.History[i].Request.Headers = append([]core.Pair(nil), w.History[i].Request.Headers...)
		w.History[i].Request.FormParts = append([]core.FormPart(nil), w.History[i].Request.FormParts...)
		w.History[i].Request.QueryParams = append([]core.QueryParam(nil), w.History[i].Request.QueryParams...)
		w.History[i].Request.Extractors = append([]core.ResponseExtractor(nil), w.History[i].Request.Extractors...)
		w.History[i].Request.Assertions = append([]core.ResponseAssertion(nil), w.History[i].Request.Assertions...)
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
	if len(w.Settings.Shortcuts) > 0 {
		shortcuts := make(map[string]string, len(w.Settings.Shortcuts))
		for action, chord := range w.Settings.Shortcuts {
			shortcuts[action] = chord
		}
		w.Settings.Shortcuts = shortcuts
	}
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
			if job.reply != nil {
				// OnBeforeQuit waits on the main thread. Return the disk result
				// independently of UI callbacks, preserving the original Relaunch.
				job.reply <- err
				continue
			}
			post(func() {
				if a.closed || job.generation != a.saveGeneration {
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
	job := saveJob{workspace: cloneWorkspace(a.workspace), revision: a.revision, generation: a.saveGeneration}
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
	a.lockSecrets()
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
	a.saveGeneration++
	job := saveJob{workspace: cloneWorkspace(a.workspace), revision: a.revision, generation: a.saveGeneration, final: true}
	select {
	case <-a.saves:
	default:
	}
	a.saves <- job
}

func (a *application) beforeQuit(e *mygo.QuitEvent) {
	if a.closed {
		return
	}
	a.lockSecrets()
	if a.saveReady || a.readOnly {
		return
	}
	a.stop()
	a.workspace.Cookies = a.engine.CookieSnapshot()
	a.closing = true
	a.saveGeneration++
	a.saveStatus = "正在保存…"
	reply := make(chan error, 1)
	job := saveJob{workspace: cloneWorkspace(a.workspace), revision: a.revision, generation: a.saveGeneration, final: true, reply: reply}
	// Replace a queued save, including a prior asynchronous window close.
	// Any in-flight save finishes first on this same worker; its UI callback
	// is invalidated by saveGeneration and need not run before the final flush.
	select {
	case <-a.saves:
	default:
	}
	a.saves <- job
	if err := <-reply; err != nil {
		e.PreventDefault()
		a.closing = false
		a.saveReady = false
		a.saveStatus = "保存失败：" + err.Error()
		a.notice = a.saveStatus
		a.post(func() {}) // repaint the retained window once the quit hook returns
		return
	}
	a.saveStatus = "已保存到本机"
	a.saveReady = true
}
func (a *application) syncDraft() {
	a.showCredentials = false
	for i := range a.workspace.Draft.Assertions {
		if a.workspace.Draft.Assertions[i].ID == "" {
			a.workspace.Draft.Assertions[i].ID = core.NewID()
		}
	}
	for i := range a.workspace.Draft.Extractors {
		if a.workspace.Draft.Extractors[i].ID == "" {
			a.workspace.Draft.Extractors[i].ID = core.NewID()
		}
	}
	for i := range a.workspace.Draft.FormParts {
		if a.workspace.Draft.FormParts[i].ID == "" {
			a.workspace.Draft.FormParts[i].ID = core.NewID()
		}
	}
	a.headers = core.FormatHeaders(a.workspace.Draft.Headers)
	a.timeout = fmt.Sprint(a.workspace.Draft.TimeoutSeconds)
	a.syncQueryParams()
}
func (a *application) syncSettings() {
	a.globalHeaders = core.FormatHeaders(a.workspace.Settings.GlobalHeaders)
	a.caCertPEM = a.workspace.Settings.CACertPEM
	a.proxyURL = a.workspace.Settings.Proxy.URL
	a.proxyBypass = a.workspace.Settings.Proxy.Bypass
	a.envName = a.workspace.Settings.Environment
	a.settingsEnvironment = a.envName
	b, _ := json.MarshalIndent(a.workspace.Settings.Environments[a.envName], "", "  ")
	a.variables = string(b)
}
func (a *application) selectRequest(r core.Request) {
	a.leaveSecretPage()
	a.stop()
	r.Headers = append([]core.Pair(nil), r.Headers...)
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	r.QueryParams = append([]core.QueryParam(nil), r.QueryParams...)
	r.Extractors = append([]core.ResponseExtractor(nil), r.Extractors...)
	r.Assertions = append([]core.ResponseAssertion(nil), r.Assertions...)
	a.workspace.Draft = r
	a.syncDraft()
	a.response = ""
	a.stream = ""
	a.responseHeaders = nil
	a.capturedResponse = responseCapture{}
	a.extractionNotice = ""
	a.resetAssertionResult()
	a.status = "准备就绪"
	a.page = 0
	a.notice = ""
	a.persist()
}
func (a *application) newRequest() {
	r := core.NewRequest()
	if a.groupExists(a.groupFilter) {
		r.GroupID = a.groupFilter
	}
	a.selectRequest(r)
}
func (a *application) saveRequest() {
	if !a.validateDraft() {
		return
	}
	r := a.workspace.Draft
	r.Headers = append([]core.Pair(nil), r.Headers...)
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	r.QueryParams = append([]core.QueryParam(nil), r.QueryParams...)
	r.Extractors = append([]core.ResponseExtractor(nil), r.Extractors...)
	r.Assertions = append([]core.ResponseAssertion(nil), r.Assertions...)
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
	if len(a.workspace.Requests) >= maxSavedRequests {
		a.notice = "收藏最多 5000 个请求"
		return
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
		if strings.HasPrefix(k, core.SecretPrefix) {
			a.notice = "secret. 为机密变量保留前缀，请在「机密变量」中配置"
			return
		}
		var text string
		if len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &text) != nil {
			a.notice = "环境变量应为 JSON 对象，值均为字符串"
			return
		}
		vars[k] = text
	}
	name := strings.TrimSpace(a.envName)
	if name == "" {
		a.notice = "请填写环境名称"
		return
	}
	// The editor holds the variables of the environment it was loaded from.
	// Saving them under the name of another existing environment would replace
	// that environment's variables, so only the loaded or a new name is allowed.
	if _, exists := a.workspace.Settings.Environments[name]; exists && name != a.settingsEnvironment {
		a.notice = fmt.Sprintf("环境「%s」已存在，请先在请求页切换到该环境再编辑", name)
		return
	}
	// Validate both before applying either, so a bad value changes nothing.
	proxy := core.ProxySettings{URL: strings.TrimSpace(a.proxyURL), Bypass: strings.TrimSpace(a.proxyBypass)}
	if err := core.ValidateProxy(proxy); err != nil {
		a.notice = err.Error()
		return
	}
	if _, err := core.ParseCACertificates(a.caCertPEM); err != nil {
		a.notice = err.Error()
		return
	}
	if err := a.engine.SetCACertificates(a.caCertPEM); err != nil {
		a.notice = err.Error()
		return
	}
	if err := a.engine.SetProxy(proxy); err != nil {
		a.notice = err.Error()
		return
	}
	a.caError = nil
	a.proxyError = nil
	a.notice = ""
	a.workspace.Settings.GlobalHeaders = h
	a.workspace.Settings.CACertPEM = a.caCertPEM
	a.workspace.Settings.Proxy = proxy
	a.workspace.Settings.Environments[name] = vars
	a.workspace.Settings.Environment = name
	a.settingsEnvironment = name
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
		a.capturedResponse.complete = false
	}
	a.busy = false
}
func (a *application) addHistory(r core.Request, status string) {
	r.Headers = append([]core.Pair(nil), r.Headers...)
	r.FormParts = append([]core.FormPart(nil), r.FormParts...)
	r.QueryParams = append([]core.QueryParam(nil), r.QueryParams...)
	r.Extractors = append([]core.ResponseExtractor(nil), r.Extractors...)
	r.Assertions = append([]core.ResponseAssertion(nil), r.Assertions...)
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
	if a.proxyError != nil {
		a.notice = a.proxyError.Error() + "；请在设置中修正或清除代理"
		return
	}
	if !a.validateDraft() {
		return
	}
	privateRequest := core.RequestUsesSecrets(a.workspace.Draft, a.workspace.Settings)
	r, err := core.ResolveWithSecrets(a.workspace.Draft, a.workspace.Settings, a.secretValues(a.workspace.Settings.Environment))
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
	r.Extractors = append([]core.ResponseExtractor(nil), r.Extractors...)
	r.Assertions = append([]core.ResponseAssertion(nil), r.Assertions...)
	environment := a.workspace.Settings.Environment
	a.capturedResponse = responseCapture{requestID: r.ID, generation: generation}
	a.extractionNotice = ""
	a.resetAssertionResult()
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
			text := runErr.Error()
			if privateRequest {
				text = "请求传输失败，请检查连接、证书和超时设置"
			}
			emit(core.Event{Kind: "error", Text: text})
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
				if a.closed || a.closing || a.generation != generation {
					return
				}
				for _, e := range batch {
					a.applyEvent(e)
				}
				if done {
					if r.Protocol == "HTTP" && ctx.Err() == nil {
						a.finishHTTPResponse(r, environment, generation)
					}
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
		a.capturedResponse.status = e.StatusCode
		a.status = fmt.Sprintf("%s  ·  %d ms", e.Status, e.Elapsed.Milliseconds())
	case "body":
		a.response = e.Text
		a.capturedResponse.body = e.RawBody
		a.capturedResponse.received = true
		a.capturedResponse.truncated = e.Truncated
		a.responseLanguage = e.Language
		a.status = fmt.Sprintf("%s  ·  %.1f KiB  ·  %d ms", strings.Split(a.status, "  ·")[0], float64(e.Bytes)/1024, e.Elapsed.Milliseconds())
		if e.Truncated {
			a.notice = "响应超过 4 MiB，当前展示已截断"
		}
	case "sse":
		a.appendStream(fmt.Sprintf("← %s  %s  id=%s\n%s\n", time.Now().Format("15:04:05"), e.Name, e.ID, e.Text))
	case "ws":
		a.appendStream("← " + time.Now().Format("15:04:05") + "\n" + e.Text + "\n")
	case "error":
		a.capturedResponse.failed = true
		a.capturedResponse.complete = false
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
	if err := core.ValidateRequestAuthentication(a.workspace.Draft); err != nil {
		a.notice = err.Error()
		return false
	}
	if err := core.ValidateRequestExtractors(a.workspace.Draft); err != nil {
		a.notice = err.Error()
		return false
	}
	if err := core.ValidateRequestAssertions(a.workspace.Draft); err != nil {
		a.notice = err.Error()
		return false
	}
	a.workspace.Draft.Headers = h
	a.workspace.Draft.TimeoutSeconds = n
	return true
}

// ownedNotice is the banner text a request input set for its invalid value.
type ownedNotice struct{ input, text string }

// setInputNotice shows input's validation message in the banner.
func (a *application) setInputNotice(input, text string) {
	a.notice = text
	a.inputNotice = ownedNotice{input, text}
}

// clearInputNotice takes the banner down once input is valid again, but only
// while the banner still shows the message input set: a notice put up since,
// by anything else, stays.
func (a *application) clearInputNotice(input string) {
	if a.inputNotice.input != input {
		return
	}
	if a.notice == a.inputNotice.text {
		a.notice = ""
	}
	a.inputNotice = ownedNotice{}
}
