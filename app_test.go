package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
)

func testApp(t *testing.T) (*application, chan func()) {
	t.Helper()
	a := newApplication(core.NewWorkspace())
	updates := make(chan func(), 32)
	a.updates = func(fn func()) { updates <- fn }
	t.Cleanup(func() { a.closed = true; a.stop(); a.engine.Close() })
	return a, updates
}
func appSnapshot(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func nextAppUpdate(t *testing.T, q <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-q:
		return fn
	case <-time.After(4 * time.Second):
		t.Fatal("异步更新没有返回")
		return nil
	}
}
func pumpApp(t *testing.T, q <-chan func(), done func() bool) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for !done() {
		select {
		case fn := <-q:
			fn()
		case <-deadline.C:
			t.Fatal("异步应用状态没有完成")
		}
	}
}
func appDraft(address string) core.Request {
	return core.Request{ID: "draft", Name: "示例", Protocol: "HTTP", Method: "GET", URL: address, TimeoutSeconds: 2, FollowRedirects: true}
}

func TestNewApplication(t *testing.T) {
	t.Run("工作区映射和编辑器初始化", func(t *testing.T) {
		w := core.NewWorkspace()
		w.Draft.Headers = []core.Pair{{Key: "X-A", Value: "one"}, {Key: "X-A", Value: "two"}}
		w.Draft.TimeoutSeconds = 9
		w.Settings.GlobalHeaders = []core.Pair{{Key: "X-G", Value: "global"}}
		a := newApplication(w)
		defer a.engine.Close()
		if a.headers != "X-A: one\nX-A: two" || a.timeout != "9" || a.globalHeaders != "X-G: global" || a.envName != w.Settings.Environment {
			t.Fatal("界面字段未恢复工作区")
		}
		var vars map[string]string
		if err := json.Unmarshal([]byte(a.variables), &vars); err != nil || !reflect.DeepEqual(vars, w.Settings.Environments[w.Settings.Environment]) {
			t.Fatal("环境字段恢复不符")
		}
		if a.responseEditor == nil || !a.responseEditor.ReadOnly || !a.responseEditor.SoftWrap {
			t.Fatal("响应编辑器配置错误")
		}
		if a.split < 100 || a.split > 500 {
			t.Fatalf("初始面板高度不合理：%v", a.split)
		}
	})
}

func TestApplicationNewRequest(t *testing.T) {
	t.Run("新建清理响应并取消旧任务", func(t *testing.T) {
		a, _ := testApp(t)
		old := a.workspace.Draft.ID
		a.response = "old"
		a.stream = "old"
		a.busy = true
		ctx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		a.newRequest()
		if a.workspace.Draft.ID == old || a.workspace.Draft.URL != "" || a.response != "" || a.stream != "" || a.busy || ctx.Err() != context.Canceled {
			t.Fatal("新建没有重置草稿和旧任务")
		}
	})
}

func TestApplicationSelectRequest(t *testing.T) {
	t.Run("恢复完整草稿且清空响应", func(t *testing.T) {
		a, _ := testApp(t)
		r := appDraft("https://example.test/new")
		r.Headers = []core.Pair{{Key: "X-A", Value: "value"}}
		a.page = 2
		a.response = "stale"
		a.responseHeaders = http.Header{"X-Old": {"value"}}
		a.selectRequest(r)
		if !reflect.DeepEqual(a.workspace.Draft, r) || a.headers != "X-A: value" || a.timeout != "2" || a.page != 0 || a.response != "" || a.responseHeaders != nil {
			t.Fatal("选中请求未完整恢复")
		}
	})
}

func TestApplicationSaveRequest(t *testing.T) {
	t.Run("新增及按ID更新收藏", func(t *testing.T) {
		a, _ := testApp(t)
		a.workspace.Draft = appDraft("https://example.test")
		a.syncDraft()
		a.headers = "X-A: one\nX-A: two"
		a.saveRequest()
		if len(a.workspace.Requests) != 1 || len(a.workspace.Requests[0].Headers) != 2 {
			t.Fatal("收藏未保存输入头")
		}
		a.workspace.Draft.Name = "更新"
		a.saveRequest()
		if len(a.workspace.Requests) != 1 || a.workspace.Requests[0].Name != "更新" {
			t.Fatal("按ID更新产生重复收藏")
		}
	})
	for _, field := range []string{"空名称", "非法头", "非法超时"} {
		t.Run(field, func(t *testing.T) {
			a, _ := testApp(t)
			a.workspace.Draft = appDraft("https://example.test")
			a.syncDraft()
			switch field {
			case "空名称":
				a.workspace.Draft.Name = " "
			case "非法头":
				a.headers = "invalid"
			case "非法超时":
				a.timeout = "bad"
			}
			a.saveRequest()
			if len(a.workspace.Requests) != 0 || a.notice == "" {
				t.Fatal("无效界面输入仍保存收藏")
			}
		})
	}
}

func TestApplicationDeleteRequest(t *testing.T) {
	t.Run("只删除目标ID", func(t *testing.T) {
		a, _ := testApp(t)
		x, y := appDraft("x"), appDraft("y")
		x.ID = "x"
		y.ID = "y"
		a.workspace.Requests = []core.Request{x, y}
		a.deleteRequest("x")
		if !reflect.DeepEqual(a.workspace.Requests, []core.Request{y}) {
			t.Fatal("删除误伤其他请求")
		}
		a.deleteRequest("missing")
		if !reflect.DeepEqual(a.workspace.Requests, []core.Request{y}) {
			t.Fatal("未知ID改变收藏")
		}
	})
}

func TestApplicationApplySettings(t *testing.T) {
	t.Run("null变量值拒绝", func(t *testing.T) {
		a, _ := testApp(t)
		before := appSnapshot(t, a.workspace.Settings)
		a.globalHeaders, a.envName, a.variables = "X-New: value", "new", `{"required":null}`
		a.applySettings()
		if appSnapshot(t, a.workspace.Settings) != before || !strings.Contains(a.notice, "字符串") {
			t.Fatalf("null不是字符串，不应静默保存为空串；提示=%q 设置=%s", a.notice, appSnapshot(t, a.workspace.Settings))
		}
	})
	t.Run("全局头与新环境原子保存", func(t *testing.T) {
		a, _ := testApp(t)
		old := a.workspace.Settings.Environment
		a.globalHeaders = "X-G: one\nX-G: two"
		a.envName = " 新环境 "
		a.variables = `{"host":"https://example.test","empty":""}`
		a.applySettings()
		s := a.workspace.Settings
		if s.Environment != "新环境" || len(s.GlobalHeaders) != 2 || s.Environments["新环境"]["host"] != "https://example.test" {
			t.Fatal("设置未完整保存")
		}
		if _, ok := s.Environments[old]; !ok {
			t.Fatal("覆盖了其他环境")
		}
	})
	for _, field := range []string{"非法头", "非字符串变量", "空对象指针", "空环境名"} {
		t.Run(field, func(t *testing.T) {
			a, _ := testApp(t)
			before := appSnapshot(t, a.workspace.Settings)
			a.globalHeaders = "X-A: value"
			a.envName = "new"
			a.variables = `{"x":"y"}`
			switch field {
			case "非法头":
				a.globalHeaders = "invalid"
			case "非字符串变量":
				a.variables = `{"x":1}`
			case "空对象指针":
				a.variables = "null"
			case "空环境名":
				a.envName = " "
			}
			a.applySettings()
			if appSnapshot(t, a.workspace.Settings) != before || a.notice == "" {
				t.Fatal("无效设置造成部分更新")
			}
		})
	}
}

func TestApplicationImportCURL(t *testing.T) {
	t.Run("导入成为草稿但不发送", func(t *testing.T) {
		a, _ := testApp(t)
		a.curlInput = `curl https://example.test/api -d 'a=1'`
		a.page = 2
		a.importCURL()
		if a.workspace.Draft.URL != "https://example.test/api" || a.workspace.Draft.Method != "POST" || a.curlInput != "" || a.page != 0 || a.busy || len(a.workspace.History) != 0 {
			t.Fatal("导入状态或无发送边界错误")
		}
	})
	t.Run("导入失败保留草稿和文本", func(t *testing.T) {
		a, _ := testApp(t)
		before := appSnapshot(t, a.workspace.Draft)
		a.curlInput = "curl --unknown https://example.test"
		a.importCURL()
		if before != appSnapshot(t, a.workspace.Draft) || a.curlInput == "" || a.notice == "" {
			t.Fatal("失败导入破坏用户输入")
		}
	})
}

func TestApplicationAddHistory(t *testing.T) {
	t.Run("最近100条且不保存响应正文", func(t *testing.T) {
		a, _ := testApp(t)
		a.response = "response-sentinel"
		for i := 0; i < 101; i++ {
			r := appDraft(fmt.Sprintf("https://example.test/%d", i))
			r.ID = fmt.Sprint(i)
			a.addHistory(r, "200 OK")
		}
		if len(a.workspace.History) != 100 || a.workspace.History[0].Request.ID != "100" || a.workspace.History[99].Request.ID != "1" {
			t.Fatal("最近100条顺序或裁剪错误")
		}
		if strings.Contains(appSnapshot(t, a.workspace.History), "response-sentinel") {
			t.Fatal("历史保存了响应正文")
		}
		if a.workspace.History[0].At.IsZero() || a.workspace.History[0].Status != "200 OK" {
			t.Fatal("历史缺少时间或状态")
		}
	})
}

func TestApplicationResponseText(t *testing.T) {
	t.Run("HTTP和流内容切换", func(t *testing.T) {
		a, _ := testApp(t)
		a.response = "body"
		a.stream = "stream"
		if a.responseText() != "body" {
			t.Fatal("HTTP内容错误")
		}
		a.workspace.Draft.Protocol = "SSE"
		if a.responseText() != "stream" {
			t.Fatal("流内容错误")
		}
	})
	t.Run("响应头排序保留重复", func(t *testing.T) {
		a, _ := testApp(t)
		a.responseTab = 1
		a.responseHeaders = http.Header{"Z-Key": {"last"}, "A-Key": {"one", "two"}}
		if a.responseText() != "A-Key: one\nA-Key: two\nZ-Key: last\n" {
			t.Fatalf("响应头文本错误：%q", a.responseText())
		}
	})
}

func TestApplicationApplyEvent(t *testing.T) {
	t.Run("响应头正文与截断提示", func(t *testing.T) {
		a, _ := testApp(t)
		a.applyEvent(core.Event{Kind: "headers", Status: "201 Created", Headers: http.Header{"X-Reply": {"yes"}}, Elapsed: 12 * time.Millisecond})
		if a.responseHeaders.Get("X-Reply") != "yes" || !strings.Contains(a.status, "201 Created") {
			t.Fatal("响应头/状态未发布")
		}
		a.applyEvent(core.Event{Kind: "body", Text: "partial", Bytes: 4096, Truncated: true, Elapsed: 20 * time.Millisecond})
		if a.response != "partial" || !strings.Contains(a.status, "4.0 KiB") || !strings.Contains(a.notice, "截断") {
			t.Fatal("响应体或截断信息丢失")
		}
	})
	t.Run("SSE和WebSocket消息呈现", func(t *testing.T) {
		a, _ := testApp(t)
		a.applyEvent(core.Event{Kind: "sse", Text: "data", Name: "update", ID: "17", Retry: "250"})
		if !strings.Contains(a.stream, "update") || !strings.Contains(a.stream, "id=17") || !strings.Contains(a.stream, "data") {
			t.Fatal("SSE可见字段丢失")
		}
		a.applyEvent(core.Event{Kind: "ws", Text: "socket-data"})
		if !strings.Contains(a.stream, "socket-data") {
			t.Fatal("WebSocket消息丢失")
		}
	})
	t.Run("错误可见且不伪造成功", func(t *testing.T) {
		a, _ := testApp(t)
		a.applyEvent(core.Event{Kind: "error", Text: "failed-sentinel"})
		if a.notice != "failed-sentinel" || a.status != "请求失败" {
			t.Fatal("错误未明确展示")
		}
	})
}

func TestApplicationAppendStream(t *testing.T) {
	t.Run("追加与容量裁剪保留最近完整行", func(t *testing.T) {
		a, _ := testApp(t)
		a.appendStream("first")
		a.appendStream("second")
		if a.stream != "first\nsecond\n" {
			t.Fatal("追加顺序改变")
		}
		a.stream = strings.Repeat("旧消息\n", 40000)
		a.appendStream("latest")
		if len(a.stream) > 256<<10 || !strings.HasSuffix(a.stream, "latest\n") || !strings.Contains(a.notice, "256 KiB") {
			t.Fatal("容量限制或最新内容丢失")
		}
	})
}

func TestApplicationPersist(t *testing.T) {
	t.Run("队列保留最新且深拷贝", func(t *testing.T) {
		a, _ := testApp(t)
		a.saves = make(chan saveJob, 1)
		a.workspace.Draft.Headers = []core.Pair{{Key: "X", Value: "old"}}
		a.workspace.Requests = []core.Request{{ID: "saved", Headers: []core.Pair{{Key: "X", Value: "old"}}}}
		a.workspace.History = []core.History{{Request: core.Request{Headers: []core.Pair{{Key: "X", Value: "old"}}}}}
		a.workspace.Settings.GlobalHeaders = []core.Pair{{Key: "X", Value: "old"}}
		a.persist()
		a.workspace.Draft.Name = "latest"
		a.persist()
		job := <-a.saves
		if len(a.saves) != 0 || job.revision != 2 || job.workspace.Draft.Name != "latest" {
			t.Fatal("保存队列没有替换成最新版本")
		}
		snapshot := appSnapshot(t, job.workspace)
		a.workspace.Draft.Headers[0].Value = "changed"
		a.workspace.Requests[0].Headers[0].Value = "changed"
		a.workspace.History[0].Request.Headers[0].Value = "changed"
		a.workspace.Settings.GlobalHeaders[0].Value = "changed"
		a.workspace.Settings.Environments[a.workspace.Settings.Environment]["changed"] = "yes"
		if appSnapshot(t, job.workspace) != snapshot {
			t.Fatal("保存快照与UI共享可变字段")
		}
	})
	for _, state := range []string{"只读", "关闭中", "已关闭"} {
		t.Run(state, func(t *testing.T) {
			a, _ := testApp(t)
			a.saves = make(chan saveJob, 1)
			switch state {
			case "只读":
				a.readOnly = true
			case "关闭中":
				a.closing = true
			case "已关闭":
				a.closed = true
			}
			a.persist()
			if len(a.saves) != 0 || a.revision != 0 {
				t.Fatal("禁止写入状态仍提交保存")
			}
		})
	}
}

func TestApplicationStartSaving(t *testing.T) {
	t.Run("最终保存成功后在UI回调关闭一次", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := testApp(t)
			var err error
			a.store, err = core.OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			closeCalls := 0
			var finalSnapshot string
			a.closeNative = func() {
				closeCalls++
				if !a.saveReady {
					t.Error("调用关闭回调前没有设置saveReady")
				}
				data, err := os.ReadFile(filepath.Join(a.store.Dir, "workspace.json"))
				if err != nil {
					t.Fatalf("关闭时最终文件尚未保存：%v", err)
				}
				var saved core.Workspace
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				if appSnapshot(t, saved) != finalSnapshot {
					t.Error("关闭回调执行时落盘内容不是最终快照")
				}
				// 模拟宿主再次发出关闭事件，验证saveReady已允许关闭。
				event := &mygo.CloseEvent{}
				a.closeWindow(event)
				if event.DefaultPrevented() {
					t.Error("最终保存完成后仍阻止关闭")
				}
			}
			a.startSaving()
			defer close(a.saves)
			a.workspace.Draft.Name = "普通保存"
			a.persist()
			synctest.Wait()
			time.Sleep(251 * time.Millisecond)
			synctest.Wait()
			if len(q) != 1 {
				t.Fatalf("普通保存应排队一个完成回调，实际%d", len(q))
			}
			(<-q)()
			if closeCalls != 0 || a.saveReady {
				t.Fatal("普通保存成功触发了关闭")
			}
			a.workspace.Draft.Name = "最终快照"
			a.workspace.Draft.Headers = []core.Pair{{Key: "X-Final", Value: "saved"}}
			a.persist()
			synctest.Wait()
			finalSnapshot = appSnapshot(t, a.workspace)
			beforeFinal := time.Now()
			event := &mygo.CloseEvent{}
			a.closeWindow(event)
			if !event.DefaultPrevented() || !a.closing {
				t.Fatal("首次关窗没有等待最终保存")
			}
			synctest.Wait()
			if time.Since(beforeFinal) != 0 || len(q) != 1 {
				t.Fatal("final没有立即替换普通合并任务并排队完成回调")
			}
			if closeCalls != 0 || a.saveReady {
				t.Fatal("后台保存绕过UI更新队列直接关闭")
			}
			(<-q)()
			synctest.Wait()
			if closeCalls != 1 || !a.saveReady || len(q) != 0 || len(a.saves) != 0 {
				t.Fatalf("最终成功只应关闭一次：calls=%d ready=%v updates=%d saves=%d", closeCalls, a.saveReady, len(q), len(a.saves))
			}
		})
	})
	t.Run("旧失败回调不得解除最终保存关闭锁", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := testApp(t)
			var err error
			a.store, err = core.OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.store.Dir, "workspace.json")
			if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			a.startSaving()
			defer close(a.saves)
			a.persist()
			synctest.Wait()
			time.Sleep(251 * time.Millisecond)
			synctest.Wait()
			oldFailure := <-q
			if err = os.Remove(path); err != nil {
				t.Fatal(err)
			}
			a.closeWindow(&mygo.CloseEvent{})
			synctest.Wait()
			finalSuccess := <-q
			oldFailure()
			if !a.closing {
				t.Error("旧非final保存失败回调解除closing，最终保存确认前重新允许编辑")
			}
			// 窗口在排队回调执行前已关闭时，忽略该完成通知。
			closeCalls := 0
			a.closeNative = func() { closeCalls++ }
			a.closed = true
			finalSuccess()
			if closeCalls != 0 {
				t.Error("已关闭窗口仍收到重复关闭调用")
			}
		})
	})
	t.Run("虚拟时间内合并修改后后台保存", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := testApp(t)
			var err error
			a.store, err = core.OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a.startSaving()
			defer close(a.saves)
			for i := 0; i < 5; i++ {
				a.workspace.Draft.Name = fmt.Sprint(i)
				a.persist()
				synctest.Wait()
			}
			if _, err = os.Stat(filepath.Join(a.store.Dir, "workspace.json")); !os.IsNotExist(err) {
				t.Fatal("合并窗口未结束就落盘")
			}
			time.Sleep(251 * time.Millisecond)
			synctest.Wait()
			if len(q) != 1 {
				t.Fatalf("应只产生一次保存结果，实际%d", len(q))
			}
			(<-q)()
			w, err := a.store.Load()
			if err != nil || w.Draft.Name != "4" || a.saveStatus != "已保存到本机" {
				t.Fatalf("合并保存失败：%v %s", err, a.saveStatus)
			}
			if _, err = os.Stat(filepath.Join(a.store.Dir, "workspace.json.bak")); !os.IsNotExist(err) {
				t.Fatal("连续编辑进行了多次落盘")
			}
		})
	})
	t.Run("旧版本完成不得显示最新已保存", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := testApp(t)
			var err error
			a.store, err = core.OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a.startSaving()
			defer close(a.saves)
			a.persist()
			synctest.Wait()
			time.Sleep(251 * time.Millisecond)
			synctest.Wait()
			old := <-q
			a.persist()
			old()
			if a.saveStatus != "正在保存…" {
				t.Fatal("旧完成事件覆盖了新版本保存状态")
			}
			synctest.Wait()
			time.Sleep(251 * time.Millisecond)
			synctest.Wait()
			(<-q)()
		})
	})
	t.Run("最终保存失败保持窗口并允许重试", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := testApp(t)
			closeCalls := 0
			a.closeNative = func() { closeCalls++ }
			var err error
			a.store, err = core.OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.store.Dir, "workspace.json")
			if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			a.startSaving()
			defer close(a.saves)
			event := &mygo.CloseEvent{}
			a.closeWindow(event)
			synctest.Wait()
			if len(q) != 1 {
				t.Fatal("final任务未立即提交保存")
			}
			(<-q)()
			if !event.DefaultPrevented() || a.closing || a.saveReady || closeCalls != 0 || !strings.Contains(a.notice, "保存失败") {
				t.Fatal("最终保存失败仍允许关闭或无法重试")
			}
			second := &mygo.CloseEvent{}
			a.closeWindow(second)
			synctest.Wait()
			if len(q) != 1 || !second.DefaultPrevented() {
				t.Fatal("失败后无法再次提交最终保存")
			}
			(<-q)()
			if closeCalls != 0 || a.saveReady || a.closing {
				t.Fatal("重试保存失败时调用关闭或错误更新了关闭状态")
			}
		})
	})
}

func TestApplicationCloseWindow(t *testing.T) {
	t.Run("最终保存替换排队内容且阻止提前关闭", func(t *testing.T) {
		a, _ := testApp(t)
		a.saves = make(chan saveJob, 1)
		a.persist()
		a.workspace.Draft.Name = "final"
		event := &mygo.CloseEvent{}
		a.closeWindow(event)
		if !event.DefaultPrevented() || !a.closing {
			t.Fatal("未等待最终保存")
		}
		job := <-a.saves
		if !job.final || job.workspace.Draft.Name != "final" {
			t.Fatal("最终保存未替换排队旧内容")
		}
		a.closeWindow(&mygo.CloseEvent{})
		if len(a.saves) != 0 {
			t.Fatal("重复关闭产生重复任务")
		}
	})
	t.Run("已保存或只读可直接关闭", func(t *testing.T) {
		for _, readOnly := range []bool{false, true} {
			a, _ := testApp(t)
			a.readOnly = readOnly
			a.saveReady = !readOnly
			event := &mygo.CloseEvent{}
			a.closeWindow(event)
			if event.DefaultPrevented() {
				t.Fatal("安全关闭被阻止")
			}
		}
	})
}

func TestApplicationSend(t *testing.T) {
	for _, field := range []string{"非法头", "非法超时", "缺失变量"} {
		t.Run(field, func(t *testing.T) {
			a, _ := testApp(t)
			a.workspace.Draft = appDraft("http://127.0.0.1:1")
			a.syncDraft()
			switch field {
			case "非法头":
				a.headers = "invalid"
			case "非法超时":
				a.timeout = "bad"
			case "缺失变量":
				a.workspace.Draft.URL = "http://127.0.0.1/{{missing}}"
			}
			a.send()
			if a.busy || len(a.workspace.History) != 0 || a.notice == "" {
				t.Fatal("无效界面输入仍开始发送")
			}
		})
	}
	t.Run("异步响应只在UI队列应用", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "reply") }))
		defer srv.Close()
		a, q := testApp(t)
		a.workspace.Draft = appDraft(srv.URL)
		a.syncDraft()
		a.send()
		fn := nextAppUpdate(t, q)
		if a.response != "" || !a.busy {
			t.Fatal("后台线程直接修改响应状态")
		}
		fn()
		pumpApp(t, q, func() bool { return !a.busy })
		if a.response != "reply" || len(a.workspace.History) != 1 {
			t.Fatal("响应或发送历史丢失")
		}
	})
	t.Run("旧请求回调不覆盖新草稿", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "old response") }))
		defer srv.Close()
		a, q := testApp(t)
		a.workspace.Draft = appDraft(srv.URL)
		a.syncDraft()
		a.send()
		old := nextAppUpdate(t, q)
		a.newRequest()
		id := a.workspace.Draft.ID
		old()
		if a.workspace.Draft.ID != id || a.response != "" || a.busy || a.status != "准备就绪" {
			t.Fatal("旧回调覆盖了新草稿")
		}
	})
	t.Run("再次发送按钮取消流任务", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: one\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer srv.Close()
		defer close(release)
		a, q := testApp(t)
		a.workspace.Draft = appDraft(srv.URL)
		a.workspace.Draft.Protocol = "SSE"
		a.syncDraft()
		a.send()
		fn := nextAppUpdate(t, q)
		a.send()
		fn()
		if a.busy || a.cancel != nil || a.status != "已停止" {
			t.Fatal("发送按钮未取消流任务")
		}
	})
}

func TestApplicationSendMessage(t *testing.T) {
	t.Run("保留发送期间新输入并记录echo", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			for {
				kind, b, err := c.Read(ctx)
				if err != nil {
					return
				}
				if err = c.Write(ctx, kind, b); err != nil {
					return
				}
			}
		}))
		defer srv.Close()
		a, q := testApp(t)
		a.workspace.Draft = appDraft("ws" + strings.TrimPrefix(srv.URL, "http"))
		a.workspace.Draft.Protocol = "WebSocket"
		a.syncDraft()
		a.send()
		pumpApp(t, q, func() bool { return a.socket != nil })
		a.message = "first"
		a.sendMessage()
		a.message = "next draft"
		pumpApp(t, q, func() bool { return strings.Contains(a.stream, "→ ") && strings.Contains(a.stream, "first") })
		if a.message != "next draft" {
			t.Fatal("发送完成覆盖了新输入")
		}
		a.stop()
	})
}
