package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func collectionNativeDocument(t *testing.T) map[string]any {
	t.Helper()
	r := NewRequest()
	r.ID, r.Name, r.URL, r.GroupID = "request", "Request", "https://example.test", "root"
	return map[string]any{
		"format": "curlgo-collection", "version": 1, "name": "Native",
		"groups":   []RequestGroup{{ID: "root", Name: "Root"}},
		"requests": []Request{r}, "rootGroupId": "root",
	}
}

func collectionNativeJSON(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestImportCurlGoCollectionRejectsInvalidDocument(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong format":           func(d map[string]any) { d["format"] = "curlgo-workspace" },
		"wrong version":          func(d map[string]any) { d["version"] = 2 },
		"missing version":        func(d map[string]any) { delete(d, "version") },
		"fractional version":     func(d map[string]any) { d["version"] = 1.5 },
		"blank name":             func(d map[string]any) { d["name"] = "  " },
		"invalid name":           func(d map[string]any) { d["name"] = "bad\nname" },
		"long name":              func(d map[string]any) { d["name"] = strings.Repeat("n", 1025) },
		"missing requests":       func(d map[string]any) { delete(d, "requests") },
		"null requests":          func(d map[string]any) { d["requests"] = nil },
		"missing groups":         func(d map[string]any) { delete(d, "groups"); d["requests"] = []Request{}; delete(d, "rootGroupId") },
		"null groups":            func(d map[string]any) { d["groups"] = nil; d["requests"] = []Request{}; delete(d, "rootGroupId") },
		"duplicate groups":       func(d map[string]any) { d["groups"] = []RequestGroup{{ID: "root", Name: "A"}, {ID: "root", Name: "B"}} },
		"missing parent":         func(d map[string]any) { d["groups"] = []RequestGroup{{ID: "root", Name: "A", ParentID: "missing"}} },
		"cycle":                  func(d map[string]any) { d["groups"] = []RequestGroup{{ID: "root", Name: "A", ParentID: "root"}} },
		"invalid group metadata": func(d map[string]any) { d["groups"] = []RequestGroup{{ID: "root", Name: "bad\nname"}} },
		"unknown root":           func(d map[string]any) { d["rootGroupId"] = "missing" },
		"non-root root": func(d map[string]any) {
			d["groups"] = []RequestGroup{{ID: "other", Name: "Other"}, {ID: "root", Name: "Root", ParentID: "other"}}
		},
		"multiple roots in selected subtree": func(d map[string]any) {
			d["groups"] = []RequestGroup{{ID: "root", Name: "Root"}, {ID: "other", Name: "Other"}}
		},
		"unknown environment":      func(d map[string]any) { d["environment"] = "Missing" },
		"null environment map":     func(d map[string]any) { d["environments"] = map[string]any{"Local": nil} },
		"invalid environment name": func(d map[string]any) { d["environments"] = map[string]any{"": map[string]string{}} },
		"invalid variable name": func(d map[string]any) {
			d["environments"] = map[string]map[string]string{"Local": {"bad\nkey": "value"}}
		},
		"unknown workspace fields": func(d map[string]any) { d["cookies"] = []any{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := collectionNativeDocument(t)
			mutate(doc)
			got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
			if err == nil || len(got.Requests) != 0 {
				t.Fatalf("invalid document imported partially: %+v %v", got, err)
			}
		})
	}
	for name, data := range map[string][]byte{
		"invalid JSON":      []byte("{"),
		"null":              []byte("null"),
		"array":             []byte("[]"),
		"trailing document": []byte(`{"format":"curlgo-collection","version":1} {}`),
		"too large":         []byte(strings.Repeat(" ", (16<<20)+1)),
		"invalid UTF-8":     []byte("{\"format\":\"curlgo-collection\",\"version\":1,\"name\":\"\xff\",\"groups\":[],\"requests\":[]}"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ImportCurlGoCollection(data); err == nil {
				t.Fatal("invalid JSON boundary accepted")
			}
		})
	}
}

func TestImportCurlGoCollectionRejectsInvalidRequests(t *testing.T) {
	cases := map[string]func(*Request){
		"missing ID":                  func(r *Request) { r.ID = "" },
		"invalid ID":                  func(r *Request) { r.ID = "bad\nid" },
		"blank name":                  func(r *Request) { r.Name = "" },
		"unknown group":               func(r *Request) { r.GroupID = "missing" },
		"unassigned selected request": func(r *Request) { r.GroupID = "" },
		"unknown protocol":            func(r *Request) { r.Protocol = "FTP" },
		"bad method":                  func(r *Request) { r.Method = "GET X" },
		"zero timeout":                func(r *Request) { r.TimeoutSeconds = 0 },
		"large timeout":               func(r *Request) { r.TimeoutSeconds = 3601 },
		"bad body mode":               func(r *Request) { r.BodyMode = "binary" },
		"large body":                  func(r *Request) { r.Body = strings.Repeat("x", MaxInput+1) },
		"large URL":                   func(r *Request) { r.URL = "https://example.test/" + strings.Repeat("x", 65536) },
		"HTTP with WS URL":            func(r *Request) { r.URL = "wss://example.test" },
		"WebSocket with HTTP URL":     func(r *Request) { r.Protocol = "WebSocket" },
		"templated FTP":               func(r *Request) { r.URL = "ftp://{{host}}/file" },
		"templated malformed URL":     func(r *Request) { r.URL = "https://bad host/{{path}}" },
		"templated malformed escape":  func(r *Request) { r.URL = "https://{{host}}/bad%zz" },
		"templated query bad escape":  func(r *Request) { r.URL = "https://{{host}}/?q=%zz" },
		"userinfo":                    func(r *Request) { r.URL = "https://user:pass@{{host}}/a" },
		"no host":                     func(r *Request) { r.URL = "https:///{{path}}" },
		"no scheme":                   func(r *Request) { r.URL = "example.test/{{path}}" },
		"bad header key":              func(r *Request) { r.Headers = []Pair{{"bad key", "value"}} },
		"bad header value":            func(r *Request) { r.Headers = []Pair{{"X", "bad\nvalue"}} },
		"reserved header":             func(r *Request) { r.Headers = []Pair{{"Content-Length", "1"}} },
		"bad form kind":               func(r *Request) { r.FormParts = []FormPart{{Kind: "binary", Name: "file"}} },
		"bad form ID":                 func(r *Request) { r.FormParts = []FormPart{{ID: "bad\nid", Kind: FormText, Name: "field"}} },
		"duplicate form IDs": func(r *Request) {
			r.FormParts = []FormPart{{ID: "same", Kind: FormText, Name: "one"}, {ID: "same", Kind: FormText, Name: "two"}}
		},
		"too many form parts": func(r *Request) {
			for range MaxFormParts + 1 {
				r.FormParts = append(r.FormParts, FormPart{Kind: FormText, Name: "field"})
			}
		},
		"relative form path": func(r *Request) {
			r.BodyMode = BodyMultipart
			r.FormParts = []FormPart{{Kind: FormFile, Name: "file", FilePath: "relative.txt"}}
		},
		"WebSocket multipart":        func(r *Request) { r.Protocol, r.URL, r.BodyMode = "WebSocket", "wss://example.test", BodyMultipart },
		"invalid query raw encoding": func(r *Request) { r.QueryParams = []QueryParam{{Key: "x", Raw: "x=%zz", HasRaw: true}} },
		"bad query ID":               func(r *Request) { r.QueryParams = []QueryParam{{ID: "bad\nid", Key: "x"}} },
		"duplicate query IDs": func(r *Request) {
			r.QueryParams = []QueryParam{{ID: "same", Key: "one"}, {ID: "same", Key: "two", Disabled: true}}
		},
		"large query metadata": func(r *Request) { r.QueryParams = []QueryParam{{Key: "x", Value: strings.Repeat("x", MaxInput+1)}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := collectionNativeDocument(t)
			requests := doc["requests"].([]Request)
			mutate(&requests[0])
			doc["requests"] = requests
			got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
			if err == nil || len(got.Requests) != 0 {
				t.Fatalf("invalid request imported: %+v %v", got, err)
			}
		})
	}
	doc := collectionNativeDocument(t)
	r := doc["requests"].([]Request)[0]
	doc["requests"] = []Request{r, r}
	if _, err := ImportCurlGoCollection(collectionNativeJSON(t, doc)); err == nil {
		t.Fatal("duplicate request IDs accepted")
	}
}

func TestImportCurlGoCollectionTemplateURLsAndInactiveFields(t *testing.T) {
	for _, tc := range []struct{ protocol, address string }{
		{"HTTP", "{{base_url}}/path?q={{q}}"},
		{"HTTP", "{{base_url}}api"},
		{"HTTP", "{{base_url}}{{endpoint}}"},
		{"HTTP", "{{ base_url }}/path"},
		{"HTTP", "{{scheme}}://{{host}}/{{path}}"},
		{"HTTP", "https://{{host}}:{{port}}/path"},
		{"SSE", "{{events_url}}"},
		{"SSE", "http://{{host}}/events"},
		{"WebSocket", "{{socket_url}}/path"},
		{"WebSocket", "wss://{{host}}:{{port}}/socket"},
	} {
		t.Run(tc.protocol+" "+tc.address, func(t *testing.T) {
			doc := collectionNativeDocument(t)
			r := doc["requests"].([]Request)[0]
			r.Protocol, r.URL, r.Body = tc.protocol, tc.address, "{{unresolved}}"
			r.FormParts = []FormPart{{ID: "inactive", Kind: FormText, Name: "inactive", Value: "{{inactive}}"}}
			doc["requests"] = []Request{r}
			got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
			if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], r) {
				t.Fatalf("compatible template or inactive editor fields changed: %+v %v", got, err)
			}
		})
	}
}

func TestImportCurlGoCollectionEmptyFoldersAndLimits(t *testing.T) {
	doc := collectionNativeDocument(t)
	doc["requests"] = []Request{}
	got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
	if err != nil || len(got.Groups) != 1 || len(got.Requests) != 0 {
		t.Fatalf("native import should retain empty folder collections: %+v %v", got, err)
	}
	r := NewRequest()
	r.URL, r.GroupID = "https://example.test", "root"
	requests := make([]Request, 5001)
	for i := range requests {
		r.ID = fmt.Sprintf("r%d", i)
		requests[i] = r
	}
	doc["requests"] = requests
	if _, err := ImportCurlGoCollection(collectionNativeJSON(t, doc)); err == nil {
		t.Fatal("accepted over 5000 native requests")
	}
	groups := make([]RequestGroup, MaxGroups+1)
	for i := range groups {
		groups[i] = RequestGroup{ID: fmt.Sprintf("g%d", i), Name: "Group"}
	}
	doc["groups"], doc["requests"], doc["rootGroupId"] = groups, []Request{}, ""
	if _, err := ImportCurlGoCollection(collectionNativeJSON(t, doc)); err == nil {
		t.Fatal("accepted too many groups")
	}
	groups = groups[:MaxGroupDepth+1]
	for i := 1; i < len(groups); i++ {
		groups[i].ParentID = groups[i-1].ID
	}
	doc["groups"] = groups
	if _, err := ImportCurlGoCollection(collectionNativeJSON(t, doc)); err == nil {
		t.Fatal("accepted excessive group depth")
	}
}

func TestImportCurlGoCollectionIndependentInputAndOutput(t *testing.T) {
	doc := collectionNativeDocument(t)
	doc["environment"] = "Local"
	doc["environments"] = map[string]map[string]string{"Local": {"payload": "line 1\nline 2\t{{template}}"}}
	data := collectionNativeJSON(t, doc)
	got, err := ImportCurlGoCollection(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = ' '
	}
	if got.Environments["Local"]["payload"] != "line 1\nline 2\t{{template}}" || got.Requests[0].URL != "https://example.test" {
		t.Fatal("import shares mutable input or changes environment values")
	}
	got.Environments["Local"]["payload"] = "changed"
	if doc["environments"].(map[string]map[string]string)["Local"]["payload"] == "changed" {
		t.Fatal("import shares environment maps")
	}
}
