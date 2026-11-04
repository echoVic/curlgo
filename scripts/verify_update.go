//go:build ignore

// Verify a release archive against the Ed25519 public key in mygo.json.
// Usage: go run scripts/verify_update.go mygo.json update-darwin-arm64.json archive.tar.gz
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

func main() {
	if err := verify(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func verify() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: verify_update config.json manifest.json archive.tar.gz")
	}
	var config struct {
		Version string `json:"version"`
		Updates struct {
			PublicKey string `json:"publicKey"`
			Github    string `json:"github"`
		} `json:"updates"`
	}
	var manifest struct {
		Version   string `json:"version"`
		URL       string `json:"url"`
		Size      int64  `json:"size"`
		Signature string `json:"signature"`
	}
	for _, item := range []struct {
		path  string
		value any
	}{{os.Args[1], &config}, {os.Args[2], &manifest}} {
		b, err := os.ReadFile(item.path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, item.value); err != nil {
			return err
		}
	}
	if manifest.Version != config.Version {
		return fmt.Errorf("manifest version %q does not match %q", manifest.Version, config.Version)
	}
	u, err := url.Parse(manifest.URL)
	if err != nil {
		return err
	}
	expectedPath := "/" + config.Updates.Github + "/releases/download/v" + config.Version + "/" + filepath.Base(os.Args[3])
	if u.Scheme != "https" || u.Host != "github.com" || u.Path != expectedPath || u.RawQuery != "" {
		return fmt.Errorf("unexpected archive URL: %s", manifest.URL)
	}
	pub, err := base64.StdEncoding.DecodeString(config.Updates.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key")
	}
	sig, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature encoding")
	}
	f, err := os.Open(os.Args[3])
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != manifest.Size {
		return fmt.Errorf("archive size %d does not match %d", n, manifest.Size)
	}
	digest := h.Sum(nil)
	if !ed25519.Verify(pub, digest, sig) {
		return fmt.Errorf("archive signature rejected")
	}
	fmt.Printf("Verified %s: version %s, %d bytes, SHA-256 %x\n", filepath.Base(os.Args[3]), manifest.Version, n, digest)
	return nil
}
