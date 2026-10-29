package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

func decodeResponseJSON(body []byte) (any, bool) {
	if !utf8.Valid(body) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document, trailing any
	if decoder.Decode(&document) != nil || decoder.Decode(&trailing) != io.EOF {
		return nil, false
	}
	return document, true
}

func lookupJSONPointer(value any, tokens []string) (any, bool) {
	for _, token := range tokens {
		switch node := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = node[token]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != token {
				return nil, false
			}
			value = node[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func responseHeaderValues(headers http.Header, name string) []string {
	var values []string
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			values = append(values, v...)
		}
	}
	return values
}
