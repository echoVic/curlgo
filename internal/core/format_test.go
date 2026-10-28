package core

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatResponseJSONPreservesSourceTokens(t *testing.T) {
	body := " \t" + `{"z":9007199254740993,"a":1.2300e+42,"z":-0,"s":"\u0061\/b","nested":[{"huge":1e9999},true,null]}` + "\r\n\t"
	want := "{\n" +
		"  \"z\": 9007199254740993,\n" +
		"  \"a\": 1.2300e+42,\n" +
		"  \"z\": -0,\n" +
		"  \"s\": \"\\u0061\\/b\",\n" +
		"  \"nested\": [\n" +
		"    {\n" +
		"      \"huge\": 1e9999\n" +
		"    },\n" +
		"    true,\n" +
		"    null\n" +
		"  ]\n" +
		"}\r\n\t"
	text, language := FormatResponse(body, "application/json")
	if text != want || language != "json" {
		t.Fatalf("FormatResponse() = (%q, %q), want (%q, json)", text, language, want)
	}
	var original, formatted bytes.Buffer
	if err := json.Compact(&original, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&formatted, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Bytes(), formatted.Bytes()) {
		t.Fatal("formatting changed JSON tokens")
	}
}

func TestFormatResponseContentType(t *testing.T) {
	body := `{"b":2,"a":1}`
	formatted := "{\n  \"b\": 2,\n  \"a\": 1\n}"
	for _, tc := range []struct {
		name, contentType, language string
	}{
		{"json", "application/json", "json"},
		{"case and quoted parameters", ` APPLICATION/JSON ; Charset="utf-8"; profile="https://example.invalid/profile;v=1" `, "json"},
		{"structured suffix", "application/problem+json", "json"},
		{"vendor suffix", "application/vnd.api+json; charset=utf-8", "json"},
		{"text suffix", "text/x-vendor+json", "json"},
		{"missing type sniffs", "", "json"},
		{"blank type sniffs", " \t", "json"},
		{"octet stream sniffs", "application/octet-stream; charset=binary", "json"},
		// Go servers label unmarked JSON text/plain, so it is sniffed too.
		{"plain text sniffs", "text/plain; charset=utf-8", "json"},
		{"unknown text stays exact", "text/csv", ""},
		{"unsupported stays exact", "application/pdf", ""},
		{"javascript stays exact", "application/javascript", "javascript"},
		{"parameter is not the media type", `text/plain; profile="application/json"`, "json"},
		{"malformed parameter keeps type", "application/json; broken", "json"},
		{"unclosed quote keeps type", `application/json; charset="utf-8`, "json"},
		{"conflicting parameters sniff", "application/json; charset=utf-8; charset=ascii", "json"},
		{"malformed media type sniffs", "application/json/extra", "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := body
			if tc.language == "json" {
				want = formatted
			}
			text, language := FormatResponse(body, tc.contentType)
			if text != want || language != tc.language {
				t.Fatalf("FormatResponse() = (%q, %q), want (%q, %q)", text, language, want, tc.language)
			}
		})
	}
}

func TestFormatResponsePlainTextStaysExact(t *testing.T) {
	// Only a JSON object or array is sniffed; other text is never touched.
	for _, body := range []string{"hello", `"just a string"`, "42", "{not json", "  spaced  \n"} {
		if text, language := FormatResponse(body, "text/plain"); text != body || language != "" {
			t.Fatalf("纯文本应原样展示：%q -> %q %q", body, text, language)
		}
	}
}

func TestFormatResponseXML(t *testing.T) {
	for _, tc := range []struct{ name, contentType, body, want string }{
		{
			name: "nested elements", contentType: "application/xml",
			body: `<?xml version="1.0"?><root a="1"><item id="x">text</item><empty/><!-- note --></root>`,
			want: "<?xml version=\"1.0\"?>\n<root a=\"1\">\n  <item id=\"x\">text</item>\n  <empty/>\n  <!-- note -->\n</root>",
		},
		{
			name: "suffix and namespaces", contentType: "application/soap+xml; charset=utf-8",
			body: `<s:Envelope xmlns:s="urn:s"><s:Body><m>1</m></s:Body></s:Envelope>`,
			want: "<s:Envelope xmlns:s=\"urn:s\">\n  <s:Body>\n    <m>1</m>\n  </s:Body>\n</s:Envelope>",
		},
		{
			name: "already indented is stable", contentType: "text/xml",
			body: "<a>\n  <b>1</b>\n</a>",
			want: "<a>\n  <b>1</b>\n</a>",
		},
		{
			// Mixed content and CDATA are whitespace-sensitive and stay intact.
			name: "mixed content untouched", contentType: "application/xml",
			body: `<p>Hello <b>big</b> world<![CDATA[ <raw> ]]></p>`,
			want: `<p>Hello <b>big</b> world<![CDATA[ <raw> ]]></p>`,
		},
		{
			name: "entities kept verbatim", contentType: "application/xml",
			body: `<a><b>&lt;&amp;&#x41;</b></a>`,
			want: "<a>\n  <b>&lt;&amp;&#x41;</b>\n</a>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, language := FormatResponse(tc.body, tc.contentType)
			if text != tc.want || language != "xml" {
				t.Fatalf("FormatResponse() = (%q, %q), want (%q, xml)", text, language, tc.want)
			}
		})
	}
	t.Run("malformed stays exact", func(t *testing.T) {
		for _, body := range []string{"<a><b></a>", "<a>", "not xml", `<a x="1></a>`} {
			if text, language := FormatResponse(body, "application/xml"); text != body || language != "xml" {
				t.Fatalf("损坏的 XML 应原样展示：%q -> %q %q", body, text, language)
			}
		}
	})
}

func TestFormatResponseHTMLKeepsSource(t *testing.T) {
	body := "<!doctype html><html><body><pre>  keep\n   spaces</pre><script>if (a<b) {}</script></body></html>"
	for _, contentType := range []string{"text/html; charset=utf-8", "application/xhtml+xml"} {
		if text, language := FormatResponse(body, contentType); text != body || language != "html" {
			t.Fatalf("HTML 应原样展示并高亮：%q %q", text, language)
		}
	}
}

func TestFormatResponseLanguages(t *testing.T) {
	for contentType, want := range map[string]string{
		"text/css": "css", "application/javascript": "javascript", "text/javascript": "javascript",
		"application/yaml": "yaml", "text/yaml": "yaml", "application/graphql-response+json": "json",
		"text/markdown": "markdown", "text/plain": "",
	} {
		if _, language := FormatResponse("x", contentType); language != want {
			t.Errorf("%s 的语言应为 %q，实际 %q", contentType, want, language)
		}
	}
}

func TestFormatResponseBounds(t *testing.T) {
	deep := strings.Repeat("<a>", 20000) + strings.Repeat("</a>", 20000)
	if text, _ := FormatResponse(deep, "application/xml"); text != deep {
		t.Fatal("嵌套过深的 XML 应原样展示，避免巨量缩进")
	}
	huge := "[" + strings.Repeat("1,", MaxBody/2) + "1]"
	if text, _ := FormatResponse(huge, "application/json"); len(text) > MaxBody*2 {
		t.Fatal("格式化结果不应超出展示上限太多")
	}
}
