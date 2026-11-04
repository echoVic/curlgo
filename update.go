package main

import (
	_ "embed"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// Only the version field is decoded; signing configuration is never shown.
//
//go:embed mygo.json
var appConfiguration []byte

func applicationVersion() string {
	if v := strings.TrimSpace(mygo.App.Version()); v != "" && v != "unknown" && v != "dev" {
		return v
	}
	var config struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(appConfiguration, &config) == nil && config.Version != "" {
		return config.Version
	}
	return "未知版本"
}

type updatePreferences struct {
	enabled, automaticChecks, automaticDownloads bool
	lastCheck                                    time.Time
}

// The adapter keeps the native plugin responsible for all network, signature,
// installation, scheduling and persistence behavior.
type updateBackend interface {
	Enabled() bool
	CheckForUpdates()
	AutomaticChecks() bool
	AutomaticDownloads() bool
	SetAutomaticChecks(bool)
	SetAutomaticDownloads(bool)
	LastCheck() time.Time
	OnChange(func()) func()
}

type nativeUpdateBackend struct{}

func (nativeUpdateBackend) Enabled() bool                 { return mygo.Updater.Enabled() }
func (nativeUpdateBackend) CheckForUpdates()              { updater.CheckForUpdates() }
func (nativeUpdateBackend) AutomaticChecks() bool         { return updater.AutomaticChecks() }
func (nativeUpdateBackend) AutomaticDownloads() bool      { return updater.AutomaticDownloads() }
func (nativeUpdateBackend) SetAutomaticChecks(on bool)    { updater.SetAutomaticChecks(on) }
func (nativeUpdateBackend) SetAutomaticDownloads(on bool) { updater.SetAutomaticDownloads(on) }
func (nativeUpdateBackend) LastCheck() time.Time          { return updater.LastCheck() }
func (nativeUpdateBackend) OnChange(fn func()) func()     { return updater.OnChange(fn) }

func configureNativeUpdater() {
	mygo.Use(native.New(updaterOptions()))
}

func updaterMenuItem() *mygo.MenuItem { return updater.MenuItem() }

func updaterOptions() updater.Options {
	return updater.Options{
		Interval: 4 * time.Hour,
		Language: "zh-Hans",
		Strings: map[string]updater.Strings{"zh-Hans": {
			Title: "CurlGo 软件更新", MenuItem: "检查更新…",
			Checking: "正在检查更新…", Cancel: "取消", OK: "知道了",
			UpToDate: "已是最新版本", UpToDateMessage: "%[1]s %[2]s 是目前可用的最新版本。",
			Unavailable:        "当前无法自动更新",
			UnavailableMessage: "%[1]s 无法在当前安装目录自行更新。请安装到有写入权限的位置；通过软件包管理器安装的应用，请使用该管理器更新。",
			DevelopmentBuild:   "开发构建不支持更新，请使用正式安装的 CurlGo。",
			Error:              "更新出错", CheckError: "检查更新失败，请检查网络后重试。", InstallError: "安装更新失败，请确认安装目录的写入权限后重试。",
			Available: "%[1]s 有新版本", AvailableMessage: "%[1]s %[2]s 已发布，当前版本为 %[3]s。是否安装更新？",
			ReleaseNotes: "更新说明", AutomaticDownloads: "以后自动下载并安装更新（由我选择开启）",
			Skip: "跳过此版本", RemindLater: "稍后提醒", Install: "安装更新",
			Downloading: "正在下载更新…", Progress: "%[1]s / %[2]s", Megabytes: "%s MB", Installing: "正在安装更新…",
			Ready: "更新已安装", ReadyMessage: "%[1]s %[2]s 将在下次启动时生效，也可以现在重新启动。",
			Later: "稍后启动", Relaunch: "立即重新启动",
		}},
	}
}

func (a *application) refreshUpdatePreferences() {
	if a.appUpdater == nil {
		return
	}
	a.updatePrefs = updatePreferences{
		enabled:            a.appUpdater.Enabled(),
		automaticChecks:    a.appUpdater.AutomaticChecks(),
		automaticDownloads: a.appUpdater.AutomaticDownloads(),
		lastCheck:          a.appUpdater.LastCheck(),
	}
}

// Called only after the GUI window is running. OnChange is delivered on the
// main thread; a queued callback may outlive unsubscription, so guard twice.
func (a *application) attachUpdater(b updateBackend) {
	a.detachUpdater()
	a.appUpdater = b
	a.refreshUpdatePreferences()
	subscription := a.updateSubscription
	a.updateOff = b.OnChange(func() {
		if a.closed || a.closing || a.updateSubscription != subscription || a.updates == nil {
			return
		}
		a.post(func() {
			if a.closed || a.closing || a.updateSubscription != subscription {
				return
			}
			a.refreshUpdatePreferences()
		})
	})
}

func (a *application) detachUpdater() {
	a.updateSubscription++
	if a.updateOff != nil {
		a.updateOff()
		a.updateOff = nil
	}
	a.appUpdater = nil
}

func (a *application) checkForUpdates() {
	if a.appUpdater != nil && !a.closed && !a.closing {
		a.appUpdater.CheckForUpdates()
	}
}

func (a *application) setAutomaticChecks(on bool) {
	if a.appUpdater == nil || a.closed || a.closing || !a.updatePrefs.enabled {
		return
	}
	a.appUpdater.SetAutomaticChecks(on)
	a.refreshUpdatePreferences()
}

func (a *application) setAutomaticDownloads(on bool) {
	if a.appUpdater == nil || a.closed || a.closing || !a.updatePrefs.enabled || !a.updatePrefs.automaticChecks {
		return
	}
	a.appUpdater.SetAutomaticDownloads(on)
	a.refreshUpdatePreferences()
}
