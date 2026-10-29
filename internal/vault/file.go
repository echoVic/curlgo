package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/echoVic/curlgo/internal/core"
)

const maxEnvelope = 2 << 20

type envelope struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return ErrFile
	}
	return nil
}

func readEnvelope(path string) (*envelope, error) {
	if path == "" {
		return nil, ErrFile
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !privateFile(info) || info.Size() > maxEnvelope {
		return nil, ErrFile
	}
	f, err := openPrivateFile(path)
	if err != nil {
		return nil, ErrFile
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !privateFile(opened) || !os.SameFile(info, opened) || opened.Size() > maxEnvelope {
		return nil, ErrFile
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEnvelope+1))
	if err != nil || len(data) > maxEnvelope {
		return nil, ErrFile
	}
	return decodeEnvelope(data)
}

func privateFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600 &&
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
}

func openPrivateFile(path string) (*os.File, error) {
	f, err := openReadFile(path)
	if err != nil {
		return nil, ErrFile
	}
	info, err := f.Stat()
	if err != nil || !privateFile(info) {
		_ = f.Close()
		return nil, ErrFile
	}
	return f, nil
}

func writeEnvelope(path string, data []byte, createOnly bool, beforeCommit func() error) error {
	if len(data) > maxEnvelope || privateDirectory(filepath.Dir(path)) != nil {
		return ErrFile
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil ||
		privateDirectory(filepath.Dir(path)) != nil {
		return ErrFile
	}
	original, err := os.Lstat(path)
	if createOnly {
		if !os.IsNotExist(err) {
			return ErrFile
		}
	} else if err != nil || !privateFile(original) {
		return ErrFile
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".curlgo-vault-*")
	if err != nil {
		return ErrFile
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil || privateDirectory(filepath.Dir(path)) != nil {
		return ErrFile
	}
	if beforeCommit != nil {
		if err = beforeCommit(); err != nil {
			return err
		}
	}
	if createOnly {
		// Link publishes without replacing a vault another opener initialized.
		err = os.Link(tmp, path)
	} else {
		current, statErr := os.Lstat(path)
		if statErr != nil || !privateFile(current) || !os.SameFile(original, current) {
			return ErrFile
		}
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return ErrFile
	}
	// Match the workspace's best-effort directory sync after atomic publication.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// Object token parsing rejects duplicate keys, null objects and null strings.
func readObject(d *json.Decoder, limit int, field func(string) error) error {
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return ErrCorrupt
	}
	seen := make(map[string]bool)
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] || len(seen) >= limit {
			return ErrCorrupt
		}
		seen[name] = true
		if err = field(name); err != nil {
			return err
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return ErrCorrupt
	}
	return nil
}

func readString(d *json.Decoder) (string, error) {
	token, err := d.Token()
	value, ok := token.(string)
	if err != nil || !ok {
		return "", ErrCorrupt
	}
	return value, nil
}

func endJSON(d *json.Decoder) error {
	if _, err := d.Token(); err != io.EOF {
		return ErrCorrupt
	}
	return nil
}

func decodeEnvelope(data []byte) (*envelope, error) {
	if !utf8.Valid(data) {
		return nil, ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	e := &envelope{}
	err := readObject(d, 4, func(name string) error {
		switch name {
		case "version":
			token, err := d.Token()
			if err != nil || token != json.Number("1") {
				return ErrCorrupt
			}
			e.Version = 1
		case "id":
			value, err := readString(d)
			if err != nil {
				return err
			}
			e.ID = value
		case "nonce", "ciphertext":
			value, err := readString(d)
			if err != nil {
				return err
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(value)
			if err != nil {
				return ErrCorrupt
			}
			if name == "nonce" {
				e.Nonce = decoded
			} else {
				e.Ciphertext = decoded
			}
		default:
			return ErrCorrupt
		}
		return nil
	})
	id, idErr := hex.DecodeString(e.ID)
	if err != nil || endJSON(d) != nil || e.Version != 1 || idErr != nil ||
		len(id) != 12 || hex.EncodeToString(id) != e.ID ||
		len(e.Nonce) != 12 || len(e.Ciphertext) < 16 {
		return nil, ErrCorrupt
	}
	return e, nil
}

func decodePlaintext(data []byte) (map[string]map[string]string, error) {
	if len(data) > core.MaxInput || !utf8.Valid(data) {
		return nil, ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(data))
	values := make(map[string]map[string]string)
	err := readObject(d, core.MaxSecrets, func(environment string) error {
		secrets := make(map[string]string)
		values[environment] = secrets
		return readObject(d, core.MaxSecrets, func(name string) error {
			value, err := readString(d)
			if err == nil {
				secrets[name] = value
			}
			return err
		})
	})
	if err != nil || endJSON(d) != nil || validateValues(values) != nil {
		return nil, ErrCorrupt
	}
	return values, nil
}

func unchangedEnvelope(path string, expected *envelope) error {
	current, err := readEnvelope(path)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrFile
	}
	if current.Version != expected.Version || current.ID != expected.ID ||
		!bytes.Equal(current.Nonce, expected.Nonce) || !bytes.Equal(current.Ciphertext, expected.Ciphertext) {
		return ErrCorrupt
	}
	return nil
}
