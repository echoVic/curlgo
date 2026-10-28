package core

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"strings"
)

// FormatResponse returns the text to display for a response body and the
// editor language that highlights it. Formatting never changes content: JSON
// is re-indented, XML only gains line breaks between tags, and anything that
// cannot be formatted safely is shown exactly as received.
func FormatResponse(body string, contentType string) (text string, language string) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil && !errors.Is(err, mime.ErrInvalidMediaParameter) {
		mediaType = ""
	}
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return formatJSON(body), "json"
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		return body, "html"
	case mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml"):
		return formatXML(body), "xml"
	case languages[mediaType] != "":
		return body, languages[mediaType]
	case mediaType == "" || mediaType == "text/plain" || mediaType == "application/octet-stream":
		// Go servers label unmarked bodies text/plain and many APIs send JSON
		// without a type, so a JSON object or array is still shown formatted.
		trimmed := strings.TrimLeft(body, " \t\r\n")
		if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(body)) {
			return formatJSON(body), "json"
		}
	}
	return body, ""
}

var languages = map[string]string{
	"text/css": "css", "application/javascript": "javascript", "text/javascript": "javascript",
	"application/yaml": "yaml", "application/x-yaml": "yaml", "text/yaml": "yaml",
	"text/markdown": "markdown", "application/graphql": "graphql",
}

// maxFormatted keeps indentation from inflating a large body without bound.
const maxFormatted = MaxBody + MaxBody/2

func formatJSON(body string) string {
	var formatted bytes.Buffer
	if json.Indent(&formatted, []byte(body), "", "  ") != nil || formatted.Len() > maxFormatted {
		return body
	}
	return formatted.String()
}

// maxXMLDepth stops pathological nesting from producing megabytes of indentation.
const maxXMLDepth = 256

type xmlToken struct {
	start, end int // byte offsets of the token in the body
	kind       int
}

const (
	xmlOpen = iota
	xmlClose
	xmlEmpty // a self-closing element
	xmlText
	xmlSpace // whitespace between markup
	xmlOther // comments, processing instructions, directives
)

// formatXML re-indents XML by changing only the whitespace between markup.
// Leaf elements, elements holding text and xml:space="preserve" elements are
// copied byte-for-byte, so text, CDATA and entities are never altered.
// Malformed XML is returned unchanged.
func formatXML(body string) string {
	d := xml.NewDecoder(strings.NewReader(body))
	d.Strict = true
	d.Entity = xml.HTMLEntity
	var tokens []xmlToken
	var names []xml.Name
	preserve := map[int]bool{}
	for {
		start := int(d.InputOffset())
		t, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return body
		}
		end := int(d.InputOffset())
		tok := xmlToken{start: start, end: end, kind: xmlOther}
		switch v := t.(type) {
		case xml.StartElement:
			if strings.HasSuffix(body[start:end], "/>") {
				tok.kind = xmlEmpty
				break
			}
			tok.kind = xmlOpen
			if names = append(names, v.Name); len(names) > maxXMLDepth {
				return body
			}
			for _, a := range v.Attr {
				if a.Name.Space == "xml" && a.Name.Local == "space" && a.Value == "preserve" {
					preserve[len(tokens)] = true
				}
			}
		case xml.EndElement:
			if start == end {
				continue // the decoder's synthetic end of a self-closing tag
			}
			if len(names) == 0 || names[len(names)-1] != v.Name {
				return body
			}
			names = names[:len(names)-1]
			tok.kind = xmlClose
		case xml.CharData:
			// Judge the source rather than the decoded text: &#32; or a CDATA
			// section of spaces is content.
			tok.kind = xmlText
			if strings.Trim(body[start:end], " \t\r\n") == "" {
				tok.kind = xmlSpace
			}
		}
		tokens = append(tokens, tok)
	}
	if len(names) != 0 {
		return body
	}
	// Mark elements to copy verbatim: those with a text child, and leaves.
	verbatim := make([]bool, len(tokens))
	hasMarkup := make([]bool, len(tokens))
	var stack []int
	for i, tok := range tokens {
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		switch tok.kind {
		case xmlOpen:
			if parent >= 0 {
				hasMarkup[parent] = true
			}
			stack = append(stack, i)
		case xmlClose:
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !hasMarkup[open] || preserve[open] {
				verbatim[open] = true
			}
		case xmlText:
			if parent >= 0 {
				verbatim[parent] = true
			}
		case xmlEmpty, xmlOther:
			if parent >= 0 {
				hasMarkup[parent] = true
			}
		}
	}
	var b strings.Builder
	level := 0
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.kind == xmlSpace {
			continue
		}
		if tok.kind == xmlClose {
			level--
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat("  ", level))
		if tok.kind == xmlOpen && verbatim[i] {
			j, nested := i, 0
			for ; j < len(tokens); j++ {
				if tokens[j].kind == xmlOpen {
					nested++
				} else if tokens[j].kind == xmlClose {
					if nested--; nested == 0 {
						break
					}
				}
			}
			b.WriteString(body[tok.start:tokens[j].end])
			i = j
		} else {
			b.WriteString(body[tok.start:tok.end])
			if tok.kind == xmlOpen {
				level++
			}
		}
		if b.Len() > maxFormatted {
			return body
		}
	}
	return b.String()
}
