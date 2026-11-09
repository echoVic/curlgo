package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The menu item must be unit-testable and open the GitHub issue form through
// the replaceable opener, so tests never launch a real browser.
func TestFeedbackMenuOpensIssueForm(t *testing.T) {
	a, _ := testApp(t)
	original := openExternal
	t.Cleanup(func() { openExternal = original })
	var opened []string
	openExternal = func(url string) error {
		opened = append(opened, url)
		return nil
	}

	item := a.feedbackMenuItem()
	if item == nil {
		t.Fatal("缺少问题反馈菜单项")
	}
	if item.Label != "问题反馈…" {
		t.Fatalf("问题反馈菜单项文案为 %q，应为 问题反馈…", item.Label)
	}
	if item.Click == nil {
		t.Fatal("问题反馈菜单项没有点击处理")
	}
	item.Click(item, nil)
	if len(opened) != 1 || opened[0] != feedbackURL {
		t.Fatalf("问题反馈菜单项打开了 %v，应为 [%s]", opened, feedbackURL)
	}
}

// Page 7 must offer the same entry without the menu. The link must target the
// issue form; the headless tester records the opened URL instead of launching
// a browser.
func TestFeedbackSettingsLinkOpensIssueForm(t *testing.T) {
	a, _ := testApp(t)
	a.page = 7
	tt := ui.NewTester(a.view, 1200, 820)
	if !tt.HasText("问题反馈") || !tt.HasText("报告问题或建议") {
		t.Fatalf("设置页没有问题反馈入口：%q", tt.Texts())
	}
	r, ok := tt.Find("报告问题或建议")
	if !ok || r.W <= 0 || r.H <= 0 || r.Y < 58 || r.Y+r.H > 820-28 {
		t.Fatalf("问题反馈入口在 1200x820 下不可见：%+v", r)
	}
	clickUI(t, tt, "报告问题或建议")
	if got := tt.OpenedURLs(); len(got) != 1 || got[0] != feedbackURL {
		t.Fatalf("设置页问题反馈入口打开了 %v，应为 [%s]", got, feedbackURL)
	}
}
