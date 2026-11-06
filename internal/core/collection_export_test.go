package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// collectionExportFixtureFile returns an absolute local file path that the
// export pipeline must validate but never read. POSIX-rooted literals such as
// "/not-present/..." are not absolute on Windows, so anchor it to os.TempDir.
func collectionExportFixtureFile() string {
	return filepath.Join(os.TempDir(), "curlgo-export-must-not-read.bin")
}

func collectionExportWorkspace() Workspace {
	raw := NewRequest()
	raw.ID, raw.Name, raw.GroupID = "raw", "Raw / literal name", "child"
	raw.Method = "PATCH"
	raw.URL = "{{base_url}}/a%2fb?tag=a%20b&tag=a+b&flag&empty=&&token={{token}}#top"
	raw.Body = "  {{payload}}\nnot-json  "
	raw.Headers = []Pair{{"X-Token", "{{token}}"}, {"x-token", "second"}, {"X-Raw", " raw "}}
	raw.QueryParams = []QueryParam{
		{ID: "q1", Key: "tag", Value: "a b", Raw: "tag=a%20b", HasRaw: true},
		{ID: "q2", Key: "tag", Value: "a b", Raw: "tag=a+b", HasRaw: true},
		{ID: "q3", Key: "off", Value: "{{off}}", Disabled: true, Raw: "off={{off}}", HasRaw: true},
	}
	raw.TimeoutSeconds, raw.FollowRedirects = 75, false
	sse := NewRequest()
	sse.ID, sse.Name, sse.GroupID = "sse", "Events", "root"
	sse.Protocol, sse.URL = "SSE", "https://{{host}}/events"
	ws := NewRequest()
	ws.ID, ws.Name, ws.Protocol = "ws", "Socket", "WebSocket"
	ws.URL, ws.Body = "wss://{{host}}/socket?q={{q}}", "{{message}}"
	upload := NewRequest()
	upload.ID, upload.Name, upload.GroupID = "upload", "Upload", "other"
	upload.Method, upload.URL, upload.BodyMode = "POST", "https://example.test/upload", BodyMultipart
	upload.FormParts = []FormPart{
		{ID: "text", Kind: FormText, Name: "note", Value: "{{note}}"},
		{ID: "file", Kind: FormFile, Name: "file", FilePath: collectionExportFixtureFile()},
	}
	return Workspace{
		Version: 1,
		Groups: []RequestGroup{
			{ID: "root", Name: "Root"},
			{ID: "child", Name: "Child", ParentID: "root"},
			{ID: "empty", Name: "Empty", ParentID: "child"},
			{ID: "other", Name: "Other"},
		},
		Requests: []Request{raw, sse, ws, upload},
		Draft:    Request{Name: "DO-NOT-EXPORT-DRAFT", Body: "draft-secret"},
		History:  []History{{Status: "history-secret"}},
		Settings: Settings{
			Environment: "Chosen",
			Environments: map[string]map[string]string{
				"Chosen": {"base_url": "https://private.test", "token": "env-secret"},
				"Other":  {"token": "other-secret"},
			},
			GlobalHeaders: []Pair{{"X-Token", "global-secret"}, {"X-Global", "{{global}}"}, {"x-global", "repeated"}},
			CACertPEM:     "ca-secret",
		},
	}
}

func TestCollectionExportNativeRoundTrip(t *testing.T) {
	w := collectionExportWorkspace()
	before, _ := json.Marshal(w)
	result, err := ExportCollection(w, CollectionExportOptions{Name: "Portable", Format: CollectionCurlGo})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestCount != 4 {
		t.Fatalf("request count = %d", result.RequestCount)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(result.Data, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["format"]) != `"curlgo-collection"` || string(document["version"]) != "1" {
		t.Fatalf("wrong native envelope: %s", result.Data)
	}
	for _, field := range []string{"draft", "history", "cookies", "settings", "proxy", "caCertPEM", "environment", "environments", "globalHeaders"} {
		if _, ok := document[field]; ok {
			t.Errorf("exported excluded field %s", field)
		}
	}
	if bytes.Contains(result.Data, []byte("secret")) || bytes.Contains(result.Data, []byte("DO-NOT-EXPORT")) {
		t.Fatalf("export leaked workspace state: %s", result.Data)
	}
	got, err := ImportCurlGoCollection(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Portable" || got.RootGroupID != "" || !reflect.DeepEqual(got.Groups, w.Groups) || !reflect.DeepEqual(got.Requests, w.Requests) {
		t.Fatalf("native round trip lost template fields: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), w.Requests[3].FormParts[1].FilePath) {
		t.Fatalf("missing multipart file-reference warning: %v", got.Warnings)
	}
	after, _ := json.Marshal(w)
	if !bytes.Equal(before, after) {
		t.Fatal("export mutated workspace")
	}
}

func TestCollectionExportInactiveBodyEditors(t *testing.T) {
	for name, r := range map[string]Request{
		"blank text row": {BodyMode: BodyRaw, FormParts: []FormPart{{ID: "part", Kind: FormText}}},
		"unpicked file":  {BodyMode: BodyRaw, FormParts: []FormPart{{ID: "part", Kind: FormFile}}},
		"inactive raw text": {
			BodyMode: BodyMultipart, Body: strings.Repeat("x", MaxInput+1),
			FormParts: []FormPart{{ID: "part", Kind: FormText, Name: "active", Value: "value"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r.ID, r.Name, r.Protocol, r.Method, r.URL, r.TimeoutSeconds = "request", name, "HTTP", "POST", "https://example.test", 30
			if _, err := Resolve(r, Settings{}); err != nil {
				t.Fatalf("inactive body editor should not block sending: %v", err)
			}
			result, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: CollectionCurlGo})
			if err != nil {
				t.Fatal(err)
			}
			got, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], r) {
				t.Fatalf("inactive body editor state was rejected or changed: %+v %v", got, err)
			}
			if _, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: CollectionPostman}); err != nil {
				t.Fatalf("inactive editor blocked Postman export: %v", err)
			}
		})
	}
}

func TestCollectionExportSelectedSubtree(t *testing.T) {
	w := collectionExportWorkspace()
	result, err := ExportCollection(w, CollectionExportOptions{Name: "Child Only", GroupID: "child", Format: CollectionCurlGo})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ImportCurlGoCollection(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestCount != 1 || got.RootGroupID != "child" || len(got.Requests) != 1 || got.Requests[0].GroupID != "child" {
		t.Fatalf("wrong subtree scope: %+v", got)
	}
	want := []RequestGroup{{ID: "child", Name: "Child"}, {ID: "empty", Name: "Empty", ParentID: "child"}}
	if !reflect.DeepEqual(got.Groups, want) || w.Groups[1].ParentID != "root" {
		t.Fatalf("selected root not promoted independently: %v", got.Groups)
	}
}

func TestCollectionExportEnvironmentOptIn(t *testing.T) {
	w := collectionExportWorkspace()
	result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo, IncludeEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ImportCurlGoCollection(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	wantHeaders := []Pair{{"X-Global", "{{global}}"}, {"x-global", "repeated"}, {"X-Token", "{{token}}"}, {"x-token", "second"}, {"X-Raw", " raw "}}
	if got.Environment != "Chosen" || !reflect.DeepEqual(got.Environments, w.Settings.Environments) || !reflect.DeepEqual(got.Requests[0].Headers, wantHeaders) {
		t.Fatalf("environment or header merge incorrect: %+v", got)
	}
	if got.Requests[0].URL != w.Requests[0].URL || got.Requests[0].Body != w.Requests[0].Body {
		t.Fatal("templates were expanded")
	}
	snapshot := append([]byte(nil), result.Data...)
	w.Groups[0].Name = "Changed"
	w.Requests[0].Headers[0].Value = "Changed"
	w.Requests[0].QueryParams[0].Raw = "Changed"
	w.Requests[3].FormParts[1].FilePath = "/Changed"
	w.Settings.Environments["Chosen"]["token"] = "Changed"
	got.Environments["Other"]["token"] = "Changed"
	got.Requests[0].Headers[0].Value = "Changed"
	if !bytes.Equal(result.Data, snapshot) {
		t.Fatal("export is not an independent byte snapshot")
	}
}

type collectionExportPMItem struct {
	Name     string                     `json:"name"`
	Item     []collectionExportPMItem   `json:"item"`
	Request  *collectionExportPMRequest `json:"request"`
	Behavior struct {
		FollowRedirects *bool `json:"followRedirects"`
	} `json:"protocolProfileBehavior"`
}

type collectionExportPMRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Header []Pair `json:"header"`
	Body   struct {
		Mode     string `json:"mode"`
		Raw      string `json:"raw"`
		FormData []struct {
			Key, Value, Type, Src string
		} `json:"formdata"`
		Options struct {
			Raw struct {
				Language string `json:"language"`
			} `json:"raw"`
		} `json:"options"`
	} `json:"body"`
}

func TestCollectionExportPostmanStructureAndLossWarnings(t *testing.T) {
	w := collectionExportWorkspace()
	result, err := ExportCollection(w, CollectionExportOptions{Name: "Postman Portable", Format: CollectionPostman, IncludeEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Info     struct{ Name, Schema string }
		Item     []collectionExportPMItem
		Variable []struct{ Key, Value, Type string }
	}
	if err := json.Unmarshal(result.Data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Info.Name != "Postman Portable" || document.Info.Schema != "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" || result.RequestCount != 2 {
		t.Fatalf("wrong Postman envelope/count: %s", result.Data)
	}
	if len(document.Item) != 2 || document.Item[0].Name != "Root" || document.Item[1].Name != "Other" {
		t.Fatalf("missing nested folders: %+v", document.Item)
	}
	child := document.Item[0].Item[0]
	if child.Name != "Child" || len(child.Item) != 2 || child.Item[0].Name != "Empty" || child.Item[0].Item == nil {
		t.Fatalf("empty folders must use item: []: %+v", child)
	}
	raw := child.Item[1]
	if raw.Name != w.Requests[0].Name || raw.Request == nil || raw.Request.Method != "PATCH" || raw.Request.URL != w.Requests[0].URL || raw.Request.Body.Raw != w.Requests[0].Body || raw.Request.Body.Mode != "raw" {
		t.Fatalf("raw request changed: %+v", raw)
	}
	if raw.Behavior.FollowRedirects == nil || *raw.Behavior.FollowRedirects {
		t.Fatal("followRedirects false was omitted")
	}
	form := document.Item[1].Item[0].Request.Body
	if form.Mode != "formdata" || len(form.FormData) != 2 || form.FormData[0].Type != "text" || form.FormData[0].Value != "{{note}}" || form.FormData[1].Type != "file" || form.FormData[1].Src != w.Requests[3].FormParts[1].FilePath {
		t.Fatalf("multipart references changed: %+v", form)
	}
	vars := map[string]string{}
	for _, v := range document.Variable {
		if v.Type != "string" {
			t.Errorf("variable type = %q", v.Type)
		}
		vars[v.Key] = v.Value
	}
	if !reflect.DeepEqual(vars, w.Settings.Environments["Chosen"]) || bytes.Contains(result.Data, []byte("other-secret")) {
		t.Fatalf("Postman should export only current environment: %v", vars)
	}
	warnings := strings.Join(result.Warnings, "\n")
	for _, text := range []string{"SSE", "WebSocket", "Events", "Socket", "75", "Query", w.Requests[0].Name} {
		if !strings.Contains(warnings, text) {
			t.Errorf("missing %q warning: %v", text, result.Warnings)
		}
	}
}

func TestCollectionExportPostmanRawRoundTripWithoutExtraContentType(t *testing.T) {
	for _, headers := range [][]Pair{nil, {{"Content-Type", "application/json"}}, {{"content-type", "application/custom"}, {"X-Two", "1"}, {"X-Two", "2"}}} {
		r := NewRequest()
		r.Name, r.Method, r.URL, r.Body = "Raw", "POST", "https://example.test/?q=a%20b&flag", " {{raw}} \n"
		r.Headers, r.FollowRedirects = headers, false
		result, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: CollectionPostman})
		if err != nil {
			t.Fatal(err)
		}
		got, err := ImportPostman(result.Data)
		if err != nil || len(got.Requests) != 1 {
			t.Fatalf("Postman round trip failed: %+v %v", got, err)
		}
		back := got.Requests[0]
		if back.Body != r.Body || back.URL != r.URL || back.Method != r.Method || back.FollowRedirects != r.FollowRedirects || !reflect.DeepEqual(back.Headers, r.Headers) {
			t.Fatalf("Postman raw round trip changed request: want %+v, got %+v", r, back)
		}
	}
}

func TestCollectionExportRejectsInvalidScopeAndFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    Workspace
		opts CollectionExportOptions
	}{
		{"empty", Workspace{}, CollectionExportOptions{Format: CollectionCurlGo}},
		{"missing group", collectionExportWorkspace(), CollectionExportOptions{Format: CollectionCurlGo, GroupID: "missing"}},
		{"empty subtree", collectionExportWorkspace(), CollectionExportOptions{Format: CollectionCurlGo, GroupID: "empty"}},
		{"unknown format", collectionExportWorkspace(), CollectionExportOptions{Format: "yaml"}},
		{"missing format", collectionExportWorkspace(), CollectionExportOptions{}},
		{"invalid name", collectionExportWorkspace(), CollectionExportOptions{Format: CollectionCurlGo, Name: strings.Repeat("x", 1025)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExportCollection(tc.w, tc.opts)
			if err == nil || len(result.Data) != 0 {
				t.Fatalf("expected no file snapshot on error: %+v %v", result, err)
			}
		})
	}
	w := collectionExportWorkspace()
	w.Requests = w.Requests[1:3]
	if result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionPostman}); err == nil || len(result.Data) != 0 {
		t.Fatalf("all skipped protocols must fail instead of writing an empty file: %+v %v", result, err)
	}
}

func TestCollectionExportNativeLimits(t *testing.T) {
	r := NewRequest()
	r.URL = "https://example.test"
	w := Workspace{}
	for i := range 5000 {
		r.ID = fmt.Sprintf("r%d", i)
		w.Requests = append(w.Requests, r)
	}
	result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo})
	if err != nil || result.RequestCount != 5000 {
		t.Fatalf("5000-request native boundary failed: %d %v", result.RequestCount, err)
	}
	got, err := ImportCurlGoCollection(result.Data)
	if err != nil || len(got.Requests) != 5000 {
		t.Fatalf("native limit is not round-trippable: %d %v", len(got.Requests), err)
	}
	r.ID = "overflow"
	w.Requests = append(w.Requests, r)
	if _, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo}); err == nil {
		t.Fatal("accepted more than 5000 native requests")
	}
	w.Requests = w.Requests[:17]
	for i := range w.Requests {
		w.Requests[i].Body = strings.Repeat("x", MaxInput)
	}
	if result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo}); err == nil || len(result.Data) != 0 {
		t.Fatal("accepted native export over 16 MiB")
	}
}

func TestCollectionExportRejectsInvalidUTF8WithoutMutation(t *testing.T) {
	w := collectionExportWorkspace()
	w.Requests[0].Body = string([]byte{0xff})
	if result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo}); err == nil || len(result.Data) != 0 {
		t.Fatal("invalid UTF-8 must not be silently replaced in a native snapshot")
	}
	if w.Requests[0].Body != string([]byte{0xff}) {
		t.Fatal("invalid input was mutated")
	}
}

func TestCollectionExportPostmanMultipartRoundTripAndSubtree(t *testing.T) {
	w := collectionExportWorkspace()
	w.Requests[3].FollowRedirects = false
	result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionPostman, GroupID: "other"})
	if err != nil || result.RequestCount != 1 {
		t.Fatalf("selected Postman export failed: %+v %v", result, err)
	}
	got, err := ImportPostman(result.Data)
	if err != nil || len(got.Requests) != 1 {
		t.Fatalf("multipart round trip failed: %+v %v", got, err)
	}
	r := got.Requests[0]
	if r.BodyMode != BodyMultipart || r.Method != "POST" || r.FollowRedirects || len(r.FormParts) != 2 || r.FormParts[0].Value != "{{note}}" || r.FormParts[1].FilePath != w.Requests[3].FormParts[1].FilePath {
		t.Fatalf("multipart request changed: %+v", r)
	}
	if len(got.Groups) != 2 || GroupPath(got.Groups, r.GroupID) != "Other / Other" {
		t.Fatalf("selected subtree not promoted: %+v", got)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), w.Requests[3].FormParts[1].FilePath) {
		t.Fatal("multipart round trip did not warn about file reference")
	}
}

func TestCollectionExportHeadersCaseInsensitiveOverride(t *testing.T) {
	w := collectionExportWorkspace()
	w.Settings.GlobalHeaders = []Pair{{"  X-Token  ", "do not include"}, {"Content-Type", "{{content_type}}"}}
	w.Requests = w.Requests[:1]
	result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo, IncludeEnvironment: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ImportCurlGoCollection(result.Data)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]Pair{{"Content-Type", "{{content_type}}"}}, w.Requests[0].Headers...)
	if !reflect.DeepEqual(got.Requests[0].Headers, want) {
		t.Fatalf("case insensitive override failed: %v", got.Requests[0].Headers)
	}
}

func TestCollectionExportOrphanGroupsAreUngroupedOnCopies(t *testing.T) {
	w := collectionExportWorkspace()
	w.Requests = w.Requests[:1]
	w.Requests[0].GroupID = "historical-missing"
	for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
		t.Run(string(format), func(t *testing.T) {
			result, err := ExportCollection(w, CollectionExportOptions{Format: format})
			if err != nil || result.RequestCount != 1 {
				t.Fatalf("historical orphan group export failed: %+v %v", result, err)
			}
			warnings := strings.Join(result.Warnings, "\n")
			if !strings.Contains(warnings, "historical-missing") || !strings.Contains(warnings, "未分组") {
				t.Fatalf("orphan normalization not explained: %v", result.Warnings)
			}
			if format == CollectionCurlGo {
				got, err := ImportCurlGoCollection(result.Data)
				if err != nil || len(got.Requests) != 1 || got.Requests[0].GroupID != "" {
					t.Fatalf("orphan must become ungrouped on the snapshot: %+v %v", got, err)
				}
			} else {
				var document struct{ Item []collectionExportPMItem }
				if err := json.Unmarshal(result.Data, &document); err != nil {
					t.Fatal(err)
				}
				last := document.Item[len(document.Item)-1]
				if last.Request == nil || last.Name != w.Requests[0].Name {
					t.Fatalf("orphan was omitted instead of exported at top level: %s", result.Data)
				}
			}
			if w.Requests[0].GroupID != "historical-missing" {
				t.Fatal("workspace's historical GroupID was changed")
			}
		})
	}
}

func TestCollectionExportPostmanMultipartStripsContentTypeOnCopies(t *testing.T) {
	for _, requestContentType := range []bool{false, true} {
		t.Run(fmt.Sprintf("request Content-Type %t", requestContentType), func(t *testing.T) {
			w := collectionExportWorkspace()
			r := w.Requests[3]
			r.GroupID = ""
			r.Headers = []Pair{{"X-Keep", "one"}, {"X-Keep", "two"}}
			if requestContentType {
				r.Headers = append(r.Headers, Pair{"cOnTent-TyPe", "text/plain"})
			}
			w.Requests, w.Groups = []Request{r}, nil
			w.Settings.GlobalHeaders = []Pair{{"CONTENT-TYPE", "application/json"}, {"X-Global", "{{global}}"}}
			before, _ := json.Marshal(w)
			result, err := ExportCollection(w, CollectionExportOptions{Format: CollectionPostman, IncludeEnvironment: true})
			if err != nil {
				t.Fatal(err)
			}
			var document struct{ Item []collectionExportPMItem }
			if err := json.Unmarshal(result.Data, &document); err != nil || len(document.Item) != 1 || document.Item[0].Request == nil {
				t.Fatalf("invalid export: %s %v", result.Data, err)
			}
			wantHeaders := []Pair{{"X-Global", "{{global}}"}, {"X-Keep", "one"}, {"X-Keep", "two"}}
			if headers := document.Item[0].Request.Header; !reflect.DeepEqual(headers, wantHeaders) {
				t.Fatalf("multipart effective headers differ: got %v, want %v", headers, wantHeaders)
			}
			after, _ := json.Marshal(w)
			if !bytes.Equal(before, after) {
				t.Fatal("Postman multipart header filtering mutated workspace")
			}
			native, err := ExportCollection(w, CollectionExportOptions{Format: CollectionCurlGo, IncludeEnvironment: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := ImportCurlGoCollection(native.Data)
			if err != nil || HTTPHeaders(got.Requests[0].Headers).Get("Content-Type") == "" {
				t.Fatalf("native export must preserve Content-Type templates: %+v %v", got, err)
			}
		})
	}
}

func TestCollectionExportPostmanRawDefaultContentTypeWarning(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		headers     []Pair
		wantWarning bool
	}{
		{"headerless raw", "{{body}}", nil, true},
		{"empty raw", "", nil, false},
		{"explicit raw", "{{body}}", []Pair{{"cOnTent-TyPe", "{{content_type}}"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRequest()
			r.Name, r.URL, r.Body, r.Headers = tc.name, "https://example.test", tc.body, tc.headers
			result, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: CollectionPostman})
			if err != nil {
				t.Fatal(err)
			}
			warnings := strings.Join(result.Warnings, "\n")
			if strings.Contains(warnings, "text/plain") != tc.wantWarning {
				t.Fatalf("Postman runtime default Content-Type warning incorrect: %v", result.Warnings)
			}
			if tc.wantWarning && (!strings.Contains(warnings, r.Name) || !strings.Contains(warnings, "Content-Type") || !strings.Contains(warnings, "可能")) {
				t.Fatalf("warning must explain possible Postman behavior: %v", result.Warnings)
			}
		})
	}
}

func TestCollectionExportSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "collection.json")
	if err := SaveCollection(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := SaveCollection(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("save failed: %q %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || posixPermissions() && info.Mode().Perm() != 0600 {
		t.Fatalf("wrong file permissions: %v %v", info, err)
	}
	if err := SaveCollection(dir, []byte("cannot overwrite directory")); err == nil {
		t.Fatal("save should report rename failure")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files leaked: %v %v", files, err)
	}
	if err := SaveCollection(filepath.Join(dir, "missing", "collection.json"), data); err == nil {
		t.Fatal("save should report missing directory")
	}
}
