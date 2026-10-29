package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxCookieFile = 2 << 20

type cookieFile struct {
	Version int           `json:"version"`
	Cookies []SavedCookie `json:"cookies"`
}

// LoadCookieFile reads a versioned cookie snapshot; a missing file returns nil, nil.
func LoadCookieFile(path string) ([]SavedCookie, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxCookieFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCookieFile {
		return nil, errors.New("cookie file exceeds 2 MiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot cookieFile
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, err
	}
	if snapshot.Version != 1 {
		return nil, fmt.Errorf("unsupported cookie file version %d", snapshot.Version)
	}
	if snapshot.Cookies == nil {
		return nil, errors.New("cookie file cookies must be a JSON array")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("cookie file contains trailing JSON data")
		}
		return nil, err
	}
	return snapshot.Cookies, nil
}

// SaveCookieFile atomically replaces a cookie snapshot only if the original is loadable.
func SaveCookieFile(path string, cookies []SavedCookie) error {
	if _, err := LoadCookieFile(path); err != nil {
		return fmt.Errorf("original cookie file cannot be overwritten: %w", err)
	}
	if cookies == nil {
		cookies = []SavedCookie{}
	}
	data, err := json.Marshal(cookieFile{Version: 1, Cookies: cookies})
	if err != nil {
		return err
	}
	if len(data) > maxCookieFile {
		return errors.New("cookie snapshot exceeds 2 MiB")
	}
	return atomicWrite(path, data)
}
