package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/echoVic/curlgo/internal/core"
)

func readSecretsFile(path string) (map[string]string, *failure) {
	if path == "" || path == "-" {
		return nil, invalidSecretsFailure()
	}
	file, err := openSecretsFile(path)
	if err != nil {
		return nil, invalidSecretsFailure()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !privateSecretsMode(info.Mode()) || info.Size() > core.MaxInput {
		return nil, invalidSecretsFailure()
	}
	data, err := readLimited(file, core.MaxInput)
	if err != nil {
		return nil, invalidSecretsFailure()
	}
	return decodeSecrets(data)
}

func decodeSecrets(data []byte) (map[string]string, *failure) {
	if len(data) > core.MaxInput || !utf8.Valid(data) || !validSecretsUnicode(data) {
		return nil, invalidSecretsFailure()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, invalidSecretsFailure()
	}
	var versionSet, variablesSet bool
	values := make(map[string]string)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, invalidSecretsFailure()
		}
		switch token {
		case "schemaVersion":
			var version int
			if versionSet || decoder.Decode(&version) != nil || version != 1 {
				return nil, invalidSecretsFailure()
			}
			versionSet = true
		case "variables":
			if variablesSet {
				return nil, invalidSecretsFailure()
			}
			variablesSet = true
			if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
				return nil, invalidSecretsFailure()
			}
			for decoder.More() {
				token, err := decoder.Token()
				name, ok := token.(string)
				if err != nil || !ok || len(values) >= core.MaxSecrets {
					return nil, invalidSecretsFailure()
				}
				if _, exists := values[name]; exists {
					return nil, invalidSecretsFailure()
				}
				var value *string
				if decoder.Decode(&value) != nil || value == nil {
					return nil, invalidSecretsFailure()
				}
				values[name] = *value
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
				return nil, invalidSecretsFailure()
			}
		default:
			return nil, invalidSecretsFailure()
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, invalidSecretsFailure()
	}
	if !versionSet || !variablesSet || decoder.Decode(new(any)) != io.EOF || core.ValidateSecretValues(values) != nil {
		return nil, invalidSecretsFailure()
	}
	return values, nil
}

// encoding/json replaces unpaired surrogate escapes instead of rejecting them.
func validSecretsUnicode(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		first, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if first < 0xd800 || first > 0xdfff {
			continue
		}
		if first >= 0xdc00 || i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		second, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || second < 0xdc00 || second > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
