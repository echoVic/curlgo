package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testCookieFileLimit = 2 << 20

func writeCookieFileFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func assertCookieFileUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("original cookie file was changed")
	}
}

func TestCookieFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	want := []SavedCookie{
		{
			URL: "https://example.com/account",
			Cookie: http.Cookie{
				Name:        "session",
				Value:       "token=\"quoted\"\\value\n",
				Quoted:      true,
				Path:        "/account",
				Domain:      ".example.com",
				Expires:     time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
				RawExpires:  "Wed, 02 Jan 2030 03:04:05 GMT",
				MaxAge:      3600,
				Secure:      true,
				HttpOnly:    true,
				SameSite:    http.SameSiteNoneMode,
				Partitioned: true,
				Raw:         "session=raw; HttpOnly",
				Unparsed:    []string{"Custom=attribute"},
			},
		},
		{URL: "http://example.org/", Cookie: http.Cookie{Name: "plain", Value: "value"}},
	}
	if err := SaveCookieFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCookieFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cookie fields changed: got=%+v want=%+v", got, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version json.RawMessage `json:"version"`
		Cookies []SavedCookie   `json:"cookies"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.Version) != "1" || !reflect.DeepEqual(envelope.Cookies, want) {
		t.Fatal("cookie file does not contain the version 1 envelope")
	}

	replacement := want[1:]
	if err := SaveCookieFile(path, replacement); err != nil {
		t.Fatal(err)
	}
	got, err = LoadCookieFile(path)
	if err != nil || !reflect.DeepEqual(got, replacement) {
		t.Fatalf("valid cookie file was not replaced: got=%+v err=%v", got, err)
	}
}

func TestCookieFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	cookies, err := LoadCookieFile(path)
	if err != nil || cookies != nil {
		t.Fatalf("missing file: got=%+v err=%v, want nil, nil", cookies, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("loading a missing file created files")
	}
}

func TestCookieFilePermissions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cookies.json")
			if existing {
				writeCookieFileFixture(t, path, []byte(`{"version":1,"cookies":[]}`))
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := SaveCookieFile(path, nil); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0600 {
				t.Fatalf("permissions=%o, want 600", got)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "cookies.json" {
				t.Fatal("save left temporary files, backups, or locks")
			}
		})
	}
}

func TestCookieFileNilCookies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	if err := SaveCookieFile(path, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 2 || string(envelope["version"]) != "1" || string(envelope["cookies"]) != "[]" {
		t.Fatalf("nil cookies must be encoded as an empty array: %s", data)
	}
	cookies, err := LoadCookieFile(path)
	if err != nil || cookies == nil || len(cookies) != 0 {
		t.Fatalf("empty array: got=%+v err=%v", cookies, err)
	}
}

func TestCookieFileRejectsInvalid(t *testing.T) {
	valid := `{"version":1,"cookies":[]}`
	tests := []struct {
		name string
		data string
	}{
		{"corrupt", "{"},
		{"unsupported_version", `{"version":2,"cookies":[]}`},
		{"fractional_version", `{"version":1.5,"cookies":[]}`},
		{"string_version", `{"version":"1","cookies":[]}`},
		{"missing_version", `{"cookies":[]}`},
		{"unknown_envelope_field", `{"version":1,"cookies":[],"extra":true}`},
		{"unknown_saved_cookie_field", `{"version":1,"cookies":[{"url":"https://example.com/","cookie":{},"extra":true}]}`},
		{"unknown_cookie_field", `{"version":1,"cookies":[{"url":"https://example.com/","cookie":{"Name":"session","extra":true}}]}`},
		{"invalid_cookie_field_type", `{"version":1,"cookies":[{"cookie":{"HttpOnly":"true"}}]}`},
		{"trailing_document", valid + "{}"},
		{"trailing_null", valid + "null"},
		{"trailing_junk", valid + "junk"},
		{"object_cookies", `{"version":1,"cookies":{}}`},
		{"string_cookies", `{"version":1,"cookies":"cookies"}`},
		{"number_cookies", `{"version":1,"cookies":1}`},
		{"boolean_cookies", `{"version":1,"cookies":true}`},
		{"null_cookies", `{"version":1,"cookies":null}`},
		{"missing_cookies", `{"version":1}`},
		{"null_envelope", "null"},
		{"array_envelope", "[]"},
		{"oversized", valid + strings.Repeat(" ", testCookieFileLimit+1-len(valid))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cookies.json")
			original := []byte(tt.data)
			writeCookieFileFixture(t, path, original)
			cookies, err := LoadCookieFile(path)
			if err == nil || cookies != nil {
				t.Fatalf("invalid file accepted: cookies=%+v err=%v", cookies, err)
			}
			assertCookieFileUnchanged(t, path, original)
			if err := SaveCookieFile(path, nil); err == nil {
				t.Fatal("save accepted an invalid original")
			}
			assertCookieFileUnchanged(t, path, original)
		})
	}
}

func TestCookieFileLoadSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	valid := `{"version":1,"cookies":[]}`
	data := []byte(valid + strings.Repeat(" ", testCookieFileLimit-len(valid)))
	writeCookieFileFixture(t, path, data)
	cookies, err := LoadCookieFile(path)
	if err != nil || cookies == nil || len(cookies) != 0 {
		t.Fatalf("file of exactly 2 MiB was rejected: cookies=%+v err=%v", cookies, err)
	}
	assertCookieFileUnchanged(t, path, data)
}

func TestCookieFileSnapshotSizeBoundary(t *testing.T) {
	for _, tt := range []struct {
		name string
		size int
	}{
		{"at_limit", testCookieFileLimit},
		{"over_limit", testCookieFileLimit + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cookies := []SavedCookie{{URL: "https://example.com/", Cookie: http.Cookie{Name: "large"}}}
			envelope := struct {
				Version int           `json:"version"`
				Cookies []SavedCookie `json:"cookies"`
			}{Version: 1, Cookies: cookies}
			data, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			cookies[0].Cookie.Value = strings.Repeat("x", tt.size-len(data))
			for _, existing := range []bool{false, true} {
				name := "new"
				if existing {
					name = "existing"
				}
				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "cookies.json")
					original := []byte(`{"version":1,"cookies":[{"url":"https://example.com/","cookie":{"Name":"old","Value":"keep","HttpOnly":true}}]}`)
					if existing {
						writeCookieFileFixture(t, path, original)
					}
					err := SaveCookieFile(path, cookies)
					if tt.size > testCookieFileLimit {
						if err == nil {
							t.Fatal("oversized snapshot was accepted")
						}
						if existing {
							assertCookieFileUnchanged(t, path, original)
						} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("rejected snapshot created a file: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if info.Size() != int64(tt.size) {
						t.Fatalf("file size=%d, want %d", info.Size(), tt.size)
					}
					got, err := LoadCookieFile(path)
					if err != nil || !reflect.DeepEqual(got, cookies) {
						t.Fatalf("snapshot at size limit did not round-trip: %v", err)
					}
				})
			}
		})
	}
}

func TestCookieFileReadErrors(t *testing.T) {
	path := t.TempDir()
	cookies, err := LoadCookieFile(path)
	var pathErr *os.PathError
	if cookies != nil || !errors.As(err, &pathErr) {
		t.Fatalf("read error was not propagated: cookies=%+v err=%v", cookies, err)
	}
	if err := SaveCookieFile(path, nil); !errors.As(err, &pathErr) {
		t.Fatalf("save did not propagate the original read error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("original directory was changed: %v", err)
	}
}

func TestCookieFileNoParentCreation(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(parent, "cookies.json")
	cookies, err := LoadCookieFile(path)
	if err != nil || cookies != nil {
		t.Fatalf("missing parent: cookies=%+v err=%v", cookies, err)
	}
	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("load created the parent: %v", err)
	}
	if err := SaveCookieFile(path, nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save without a parent must fail: %v", err)
	}
	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save created the parent: %v", err)
	}
}
