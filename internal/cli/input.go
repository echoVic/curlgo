package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/echoVic/curlgo/internal/core"
)

type inputData struct {
	Request  core.Request
	Settings core.Settings
}

type requestEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Request       json.RawMessage `json:"request"`
	Settings      core.Settings   `json:"settings"`
}

func readLimited(r io.Reader, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("invalid input size limit")
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errInputTooLarge
	}
	return data, nil
}

func decodeOne(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("input must contain one JSON document")
	}
	return nil
}

func decodeInput(data []byte, asCurl bool) (inputData, *failure) {
	if len(data) > core.MaxInput {
		return inputData{}, inputFailure("INVALID_INPUT", "Input exceeds 1 MiB. Reduce the input size.")
	}
	var input inputData
	if asCurl {
		request, err := core.ImportCURL(string(data))
		if err != nil {
			return inputData{}, inputFailure("INVALID_CURL", "Invalid or unsupported curl input. Use supported curl options without shell execution.")
		}
		input.Request = request
	} else {
		var envelope requestEnvelope
		if err := decodeOne(data, &envelope); err != nil {
			return inputData{}, inputFailure("INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.")
		}
		if envelope.SchemaVersion != 1 {
			return inputData{}, inputFailure("INVALID_INPUT", "Unsupported schema version. Use schemaVersion 1.")
		}
		if len(envelope.Request) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Request), []byte("null")) {
			return inputData{}, inputFailure("INVALID_INPUT", "Missing request object. Provide a non-null request object.")
		}
		input.Request = core.Request{
			Method: "GET", Protocol: "HTTP", TimeoutSeconds: 30, FollowRedirects: true,
		}
		if err := decodeOne(envelope.Request, &input.Request); err != nil {
			return inputData{}, inputFailure("INVALID_INPUT", "Invalid request JSON fields. Check field types and supported fields.")
		}
		input.Settings = envelope.Settings
	}
	if input.Request.Protocol != "HTTP" {
		return inputData{}, inputFailure("INVALID_INPUT", "Unsupported request protocol. Use HTTP.")
	}
	request, err := core.Resolve(input.Request, input.Settings)
	if err != nil {
		var unresolved *core.UnresolvedVariableError
		if errors.As(err, &unresolved) {
			return inputData{}, inputFailure("UNRESOLVED_VARIABLE", "Unresolved request variable.")
		}
		return inputData{}, inputFailure("INVALID_REQUEST", "Invalid request.")
	}
	input.Request = request
	return input, nil
}
