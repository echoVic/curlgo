package core

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

type SavedCookie struct {
	URL    string      `json:"url"`
	Cookie http.Cookie `json:"cookie"`
}

type cookieEntry struct {
	saved SavedCookie
	// order is creation order, which the restored jar uses for send order;
	// updated picks which cookies survive the persistence size limit.
	order, updated uint64
}

// Keep the standard jar responsible for RFC domain/path selection. Its Cookies
// method intentionally omits the metadata needed to restore cookies on restart.
type persistentJar struct {
	mu      sync.Mutex
	jar     *cookiejar.Jar
	entries map[string]cookieEntry
	next    uint64
	changed func()
}

func newCookieJar(changed func()) *persistentJar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &persistentJar{jar: jar, entries: make(map[string]cookieEntry), changed: changed}
}

func (j *persistentJar) Cookies(u *url.URL) []*http.Cookie { return j.jar.Cookies(u) }

func (j *persistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return
	}
	host, err := idna.ToASCII(strings.TrimSuffix(u.Hostname(), "."))
	if err != nil || host == "" {
		return
	}
	host = strings.ToLower(host)
	now := time.Now()
	j.mu.Lock()
	modified := false
	for _, original := range cookies {
		if original == nil {
			continue
		}
		c := *original
		// Expires before 1601 is invalid but still means "delete", so validate
		// everything else here and let the expiry check below handle it.
		valid := c
		valid.Expires = time.Time{}
		if valid.Valid() != nil {
			continue
		}
		c.Raw, c.RawExpires, c.Unparsed = "", "", nil
		if c.Path == "" || c.Path[0] != '/' {
			c.Path = "/"
			if i := strings.LastIndex(u.Path, "/"); i > 0 && u.Path[0] == '/' {
				c.Path = u.Path[:i]
			}
		}
		// A disposable standard jar validates domain acceptance even for
		// deletion or Secure cookies that are not visible at the source URL.
		probe := newCookieJar(nil).jar
		check := c
		check.MaxAge, check.Expires, check.Secure = 0, time.Time{}, false
		probe.SetCookies(u, []*http.Cookie{&check})
		probeURL := *u
		probeURL.Path = c.Path
		if len(probe.Cookies(&probeURL)) == 0 {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		if domain == "" {
			domain = host
		}
		if net.ParseIP(host) != nil {
			c.Domain = ""
		}
		key := domain + "\x00" + c.Path + "\x00" + c.Name
		remove := c.MaxAge < 0
		if c.MaxAge > 0 {
			// Convert once so relaunching never renews a server's Max-Age.
			seconds := int64(c.MaxAge)
			if seconds > 100*365*24*60*60 {
				seconds = 100 * 365 * 24 * 60 * 60
			}
			c.Expires = now.Add(time.Duration(seconds) * time.Second)
		} else if !c.Expires.IsZero() && !c.Expires.After(now) {
			remove = true
		}
		c.MaxAge = 0
		if remove {
			c.MaxAge = -1
			delete(j.entries, key)
		} else {
			entry, exists := j.entries[key]
			if !exists {
				entry.order = j.next
			}
			entry.updated = j.next
			j.next++
			source := url.URL{Scheme: u.Scheme, Host: host, Path: c.Path}
			if strings.Contains(host, ":") {
				source.Host = "[" + host + "]"
			}
			entry.saved = SavedCookie{URL: source.String(), Cookie: c}
			j.entries[key] = entry
		}
		j.jar.SetCookies(u, []*http.Cookie{&c})
		modified = true
	}
	j.mu.Unlock()
	if modified && j.changed != nil {
		j.changed()
	}
}

func (j *persistentJar) snapshot() []SavedCookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	entries := make([]cookieEntry, 0, len(j.entries))
	for key, entry := range j.entries {
		if expires := entry.saved.Cookie.Expires; !expires.IsZero() && !expires.After(now) {
			delete(j.entries, key)
			continue
		}
		entries = append(entries, entry)
	}
	// Persist at most 1 MiB so cookies cannot push the workspace past its
	// limit; the live session keeps every cookie.
	sort.Slice(entries, func(i, k int) bool { return entries[i].updated > entries[k].updated })
	size := 0
	kept := entries[:0]
	for _, entry := range entries {
		data, err := json.Marshal(entry.saved)
		if err != nil {
			continue
		}
		size += len(data)
		if size > MaxInput {
			break
		}
		kept = append(kept, entry)
	}
	sort.Slice(kept, func(i, k int) bool { return kept[i].order < kept[k].order })
	var out []SavedCookie
	for _, entry := range kept {
		out = append(out, entry.saved)
	}
	return out
}

func (e *Engine) cookiesChanged() {
	e.mu.RLock()
	fn := e.cookieChanged
	e.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (e *Engine) OnCookiesChanged(fn func()) {
	e.mu.Lock()
	e.cookieChanged = fn
	e.mu.Unlock()
}

func (e *Engine) CookieSnapshot() []SavedCookie {
	e.mu.RLock()
	jar := e.jar
	e.mu.RUnlock()
	return jar.snapshot()
}

func (e *Engine) RestoreCookies(cookies []SavedCookie) {
	jar := newCookieJar(nil)
	for _, saved := range cookies {
		u, err := url.Parse(saved.URL)
		if err == nil && u.User == nil {
			jar.SetCookies(u, []*http.Cookie{&saved.Cookie})
		}
	}
	jar.changed = e.cookiesChanged
	e.mu.Lock()
	e.jar = jar
	e.mu.Unlock()
}
