package main

import "github.com/egoist/mygo"

// feedbackURL is the GitHub issue form users land on to report a problem or
// suggest an improvement.
const feedbackURL = "https://github.com/echoVic/curlgo/issues/new"

// openExternal opens a URL in the system browser. Tests replace it so they
// never launch a real browser.
var openExternal = mygo.Shell.OpenExternal

// openFeedback opens the GitHub issue form. A failed launch is not actionable
// for the user here, so the error is ignored.
func (a *application) openFeedback() { _ = openExternal(feedbackURL) }

// feedbackMenuItem builds the menu entry for the feedback page. It is kept out
// of installMenu so tests can invoke its click handler without a native window.
func (a *application) feedbackMenuItem() *mygo.MenuItem {
	return &mygo.MenuItem{Label: "问题反馈…", Click: func(*mygo.MenuItem, *mygo.Window) {
		a.openFeedback()
	}}
}
