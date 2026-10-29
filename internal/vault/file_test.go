package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func marshalTestEnvelope(t *testing.T, e envelope) []byte {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMutationNeverOverwritesChangedOrCorruptFile(t *testing.T) {
	for _, kind := range []string{"ciphertext", "malformed", "missing", "replacement", "public file"} {
		t.Run(kind, func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			unlockTestVault(t, v)
			if err := v.Set("dev", "token", "private-original"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "ciphertext":
				e := readTestEnvelope(t, path)
				e.Ciphertext[0] ^= 1
				writeTestFile(t, path, marshalTestEnvelope(t, e))
			case "malformed":
				writeTestFile(t, path, []byte(`{"private-input":"private-file-value"}`))
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				other := openTestVault(t, testPath(t), keys)
				unlockTestVault(t, other)
				data, _ := os.ReadFile(other.path)
				writeTestFile(t, path, data)
			case "public file":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			if err := v.Set("dev", "token", "private-replacement"); err == nil {
				t.Fatal("changed vault overwritten")
			} else if strings.Contains(err.Error(), "private-input") || strings.Contains(err.Error(), "private-file-value") {
				t.Fatal("changed file echoed in error")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) ||
				!reflect.DeepEqual(v.Values("dev"), map[string]string{"token": "private-original"}) {
				t.Fatal("failed changed-file write modified disk or memory")
			}
		})
	}
}

func TestUnlockRejectsFileChangedDuringLoad(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	unlockTestVault(t, v)
	v.Lock()
	keys.onLoad = func() { writeTestFile(t, path, []byte(`{}`)) }
	if err := v.Unlock(context.Background()); err == nil || v.Unlocked() {
		t.Fatal("unlock installed stale data after file corruption")
	}
	if keys.creates != 1 || keys.removes != 0 {
		t.Fatal("changed-file load reset key")
	}
}

func TestInitializationDoesNotOverwriteConcurrentVault(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	first := openTestVault(t, path, keys)
	started, release := make(chan struct{}), make(chan struct{})
	keys.onCreate = func() {
		keys.mu.Lock()
		keys.onCreate = nil
		keys.mu.Unlock()
		close(started)
		<-release
	}
	done := make(chan error, 1)
	go func() { done <- first.Unlock(context.Background()) }()
	<-started
	second := openTestVault(t, path, keys)
	err := second.Unlock(context.Background())
	close(release)
	firstErr := <-done
	if err != nil {
		t.Fatal(err)
	}
	if firstErr == nil || first.Unlocked() || !second.Unlocked() {
		t.Fatal("competing initialization replaced the existing vault")
	}
	e := readTestEnvelope(t, path)
	if keys.creates != 2 || keys.removes != 1 || len(keys.keys) != 1 || len(keys.keys[e.ID]) != 32 {
		t.Fatal("rollback removed the competing vault's key")
	}
}

func TestAtomicWriteFailureCleansTemporaryFile(t *testing.T) {
	path := testPath(t)
	original := validEnvelopeJSON(t)
	writeTestFile(t, path, original)
	err := writeEnvelope(path, []byte("unpublished"), false, func() error { return ErrCanceled })
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("precommit cancellation: %v", err)
	}
	after, _ := os.ReadFile(path)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("failed commit changed file or left temporary data")
	}
}

func TestBoundaryMetadataAndKeyClearing(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	unlockTestVault(t, v)
	env, name, value := strings.Repeat("e", 1024), strings.Repeat("n", 256), strings.Repeat("x", 65536)
	if err := v.Set(env, name, value); err != nil {
		t.Fatal(err)
	}
	if v.Values(env)[name] != value {
		t.Fatal("valid boundary value changed")
	}
	before, _ := os.ReadFile(path)
	for _, bad := range []struct{ env, name string }{{"", name}, {env, ""}, {env, "secret.token"}, {"dev\n", name}} {
		if err := v.Delete(bad.env, bad.name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid delete metadata: %v", err)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid delete changed file")
	}
	ownedKey := v.key
	v.Lock()
	if !bytes.Equal(ownedKey, make([]byte, 32)) || v.values != nil || v.key != nil {
		t.Fatal("Lock did not clear owned key bytes and drop values")
	}
}
