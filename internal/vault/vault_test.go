package vault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeKeyStore struct {
	mu        sync.Mutex
	keys      map[string][]byte
	creates   int
	loads     int
	removes   int
	createErr error
	loadErr   error
	removeErr error
	onCreate  func()
	onLoad    func()
}

func (s *fakeKeyStore) Create(ctx context.Context, id string, key []byte) error {
	s.mu.Lock()
	s.creates++
	err := s.createErr
	if err == nil {
		if s.keys == nil {
			s.keys = make(map[string][]byte)
		}
		if _, exists := s.keys[id]; exists {
			err = errors.New("duplicate private key")
		} else {
			s.keys[id] = bytes.Clone(key)
		}
	}
	hook := s.onCreate
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func (s *fakeKeyStore) Load(ctx context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	s.loads++
	key, err, hook := bytes.Clone(s.keys[id]), s.loadErr, s.onLoad
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("missing private key")
	}
	return key, nil
}

func (s *fakeKeyStore) Remove(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removes++
	if s.removeErr != nil {
		return s.removeErr
	}
	delete(s.keys, id)
	return nil
}

func testPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "secret-vault.json")
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func validEnvelopeJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"version": 1, "id": "0123456789abcdef01234567",
		"nonce": make([]byte, 12), "ciphertext": make([]byte, 16),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOpenIsReadOnlyAndNeverAccessesKeys(t *testing.T) {
	keys := &fakeKeyStore{}
	path := filepath.Join(t.TempDir(), "absent", "secret-vault.json")
	v, err := Open(path, keys)
	if err != nil || v == nil {
		t.Fatalf("Open missing vault: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("Open created a directory: %v", err)
	}
	path = testPath(t)
	writeTestFile(t, path, validEnvelopeJSON(t))
	if _, err := Open(path, keys); err != nil {
		t.Fatalf("Open valid envelope: %v", err)
	}
	if keys.creates != 0 || keys.loads != 0 || keys.removes != 0 {
		t.Fatal("Open accessed key store")
	}
}

func TestOpenRejectsUnsafeOrMalformedFiles(t *testing.T) {
	for _, kind := range []string{
		"public file", "public directory", "symlink", "directory", "oversize",
		"empty", "unknown field", "duplicate field", "trailing", "bad version",
		"bad id", "bad nonce", "short ciphertext", "null", "invalid utf8", "wrong type",
	} {
		t.Run(kind, func(t *testing.T) {
			path := testPath(t)
			data := validEnvelopeJSON(t)
			switch kind {
			case "oversize":
				data = bytes.Repeat([]byte(" "), (2<<20)+1)
			case "empty":
				data = nil
			case "unknown field":
				data = append([]byte(`{"private_input":"private-value",`), data[1:]...)
			case "duplicate field":
				data = append([]byte(`{"version":1,`), data[1:]...)
			case "trailing":
				data = append(data, []byte(` {}`)...)
			case "bad version":
				data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)
			case "bad id":
				data = bytes.Replace(data, []byte("0123456789abcdef01234567"), []byte("private-invalid-id"), 1)
			case "bad nonce":
				data = bytes.Replace(data, []byte(`"nonce":"AAAAAAAAAAAAAAAA"`), []byte(`"nonce":"AA=="`), 1)
			case "short ciphertext":
				data = bytes.Replace(data, []byte(`"ciphertext":"AAAAAAAAAAAAAAAAAAAAAA=="`), []byte(`"ciphertext":"AA=="`), 1)
			case "null":
				data = []byte(`null`)
			case "invalid utf8":
				data = append(data, 0xff)
			case "wrong type":
				data = bytes.Replace(data, []byte(`"nonce":"AAAAAAAAAAAAAAAA"`), []byte(`"nonce":[0,0,0,0,0,0,0,0,0,0,0,0]`), 1)
			}
			writeTestFile(t, path, data)
			switch kind {
			case "public file":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "public directory":
				if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := path + ".target"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			keys := &fakeKeyStore{}
			if _, err := Open(path, keys); err == nil {
				t.Fatal("unsafe vault accepted")
			} else if strings.Contains(err.Error(), "private-value") ||
				strings.Contains(err.Error(), "private_input") ||
				strings.Contains(err.Error(), "private-invalid-id") ||
				strings.Contains(err.Error(), path) {
				t.Fatalf("unsafe error: %v", err)
			}
			if keys.creates != 0 || keys.loads != 0 || keys.removes != 0 {
				t.Fatal("invalid Open accessed keys")
			}
		})
	}
}

func openTestVault(t *testing.T, path string, keys *fakeKeyStore) *Vault {
	t.Helper()
	v, err := Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func unlockTestVault(t *testing.T, v *Vault) {
	t.Helper()
	if err := v.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func readTestEnvelope(t *testing.T, path string) envelope {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestUnlockInitializesPrivateEncryptedVault(t *testing.T) {
	path := filepath.Join(filepath.Dir(testPath(t)), "new", "secret-vault.json")
	keys := &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	if v.Unlocked() || v.Values("dev") != nil {
		t.Fatal("new vault is not locked")
	}
	unlockTestVault(t, v)
	if !v.Unlocked() || keys.creates != 1 || keys.loads != 0 || keys.removes != 0 {
		t.Fatal("initialization did not create exactly one key")
	}
	e := readTestEnvelope(t, path)
	key := keys.keys[e.ID]
	if len(key) != 32 {
		t.Fatal("not an AES-256 key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, e.Nonce, e.Ciphertext, []byte("curlgo.secret-vault\x00v1\x00"+e.ID))
	if err != nil || string(plain) != "{}" {
		t.Fatalf("initial ciphertext/AAD invalid: %v", err)
	}
	for _, target := range []string{filepath.Dir(path), path} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		expected := os.FileMode(0600)
		if info.IsDir() {
			expected = 0700
		}
		if info.Mode().Perm() != expected {
			t.Fatalf("unsafe permissions: %o", info.Mode().Perm())
		}
	}
	v.Lock()
	if v.Unlocked() || v.Values("dev") != nil {
		t.Fatal("Lock retained access")
	}
	reopened := openTestVault(t, path, keys)
	unlockTestVault(t, reopened)
	if keys.creates != 1 || keys.loads != 1 || keys.removes != 0 {
		t.Fatal("reopen reinitialized vault")
	}
}

func TestUnlockFailureNeverResetsExistingVault(t *testing.T) {
	for _, kind := range []string{"missing key", "short key", "load error", "ciphertext", "id", "missing file", "context"} {
		t.Run(kind, func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			unlockTestVault(t, v)
			v.Lock()
			e := readTestEnvelope(t, path)
			switch kind {
			case "missing key":
				delete(keys.keys, e.ID)
			case "short key":
				keys.keys[e.ID] = make([]byte, 31)
			case "load error":
				keys.loadErr = errors.New("private-platform-error")
			case "ciphertext":
				e.Ciphertext[0] ^= 1
				data, _ := json.Marshal(e)
				writeTestFile(t, path, data)
			case "id":
				old := e.ID
				e.ID = "abcdef0123456789abcdef01"
				keys.keys[e.ID] = bytes.Clone(keys.keys[old])
				data, _ := json.Marshal(e)
				writeTestFile(t, path, data)
			case "missing file":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "context" {
				keys.onLoad = cancel
			}
			if err := v.Unlock(ctx); err == nil {
				t.Fatal("invalid existing vault unlocked")
			} else if strings.Contains(err.Error(), "private-platform-error") {
				t.Fatal("native error leaked")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || keys.creates != 1 || keys.removes != 0 ||
				v.Unlocked() || v.Values("dev") != nil {
				t.Fatal("failure replaced or exposed existing vault")
			}
		})
	}
}

func TestUnlockInitializationRollbackOnlyRemovesCreatedKey(t *testing.T) {
	for _, kind := range []string{"context", "write failure", "cleanup failure", "create failure", "already canceled"} {
		t.Run(kind, func(t *testing.T) {
			path := testPath(t)
			keys := &fakeKeyStore{keys: map[string][]byte{"preexisting": make([]byte, 32)}}
			v := openTestVault(t, path, keys)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "context", "cleanup failure":
				keys.onCreate = cancel
				if kind == "cleanup failure" {
					keys.removeErr = errors.New("private-cleanup-error")
				}
			case "write failure":
				keys.onCreate = func() {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Error(err)
					}
				}
			case "create failure":
				keys.createErr = errors.New("private-create-error")
			case "already canceled":
				cancel()
			}
			if err := v.Unlock(ctx); err == nil {
				t.Fatal("failed initialization succeeded")
			} else if strings.Contains(err.Error(), "private-") {
				t.Fatalf("rollback error leaked data: %v", err)
			}
			wantRemove := 1
			if kind == "create failure" || kind == "already canceled" {
				wantRemove = 0
			}
			if keys.removes != wantRemove || keys.keys["preexisting"] == nil || v.Unlocked() {
				t.Fatal("rollback touched a preexisting key or retained access")
			}
			if kind != "cleanup failure" && len(keys.keys) != 1 {
				t.Fatal("created key orphaned")
			}
			if kind != "write failure" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("failed initialization published file")
				}
			}
		})
	}
}

func TestLockInvalidatesInFlightUnlock(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		t.Run(map[bool]string{false: "load", true: "create"}[initialize], func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			if !initialize {
				unlockTestVault(t, v)
				v.Lock()
			}
			started, release := make(chan struct{}), make(chan struct{})
			hook := func() { close(started); <-release }
			if initialize {
				keys.onCreate = hook
			} else {
				keys.onLoad = hook
			}
			done := make(chan error, 1)
			go func() { done <- v.Unlock(context.Background()) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("key store not reached")
			}
			locked := make(chan struct{})
			go func() { v.Lock(); close(locked) }()
			select {
			case <-locked:
			case <-time.After(3 * time.Second):
				close(release)
				<-done
				t.Fatal("Lock blocked on key store")
			}
			close(release)
			if err := <-done; err == nil || v.Unlocked() || v.Values("dev") != nil {
				t.Fatal("in-flight unlock restored locked state")
			}
			if initialize {
				if keys.removes != 1 || len(keys.keys) != 0 {
					t.Fatal("invalidated initialization did not roll back key")
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("invalidated initialization published file")
				}
			} else if keys.removes != 0 {
				t.Fatal("invalidated load removed existing key")
			}
		})
	}
}

func TestUnlockRejectsInvalidPlaintext(t *testing.T) {
	for _, plain := range []string{
		`{"dev":null}`, `{"dev":{"token":null}}`, `{"dev":{"token":123}}`,
		`{"dev":{"secret.token":"private-plaintext"}}`, `{"":{"token":"private-plaintext"}}`,
		`{"dev\n":{"token":"private-plaintext"}}`, `{"dev":{"bad name":"private-plaintext"}}`,
		`{"dev":{},"dev":{}}`, `{"dev":{"token":"first","token":"private-plaintext"}}`,
		`{} {}`, `[]`, `null`, "{\"dev\":{\"token\":\"private-plaintext\xff\"}}",
		`{"dev":{"token":"` + strings.Repeat("x", 65537) + `"}}`,
		strings.Repeat(" ", (1<<20)+1) + `{}`,
	} {
		t.Run(string(plain[:min(len(plain), 40)]), func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			unlockTestVault(t, v)
			v.Lock()
			e := readTestEnvelope(t, path)
			_, data, err := encrypt(keys.keys[e.ID], e.ID, []byte(plain))
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, path, data)
			if err := v.Unlock(context.Background()); err == nil {
				t.Fatal("authenticated but invalid plaintext accepted")
			} else if strings.Contains(err.Error(), "private-plaintext") {
				t.Fatal("plaintext echoed in error")
			}
			after, _ := os.ReadFile(path)
			if v.Unlocked() || !bytes.Equal(data, after) || keys.creates != 1 || keys.removes != 0 {
				t.Fatal("invalid plaintext reset or exposed vault")
			}
		})
	}
}

func TestSetBatchPersistsIsolatedClonedValues(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	unlockTestVault(t, v)
	if err := v.Set("private-dev", "private_token", "private-value\nbody"); err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"private_token": "private-replaced", "other": "private-other"}
	if err := v.SetBatch("private-dev", input); err != nil {
		t.Fatal(err)
	}
	input["private_token"] = "caller-mutation"
	if err := v.Set("private-prod", "private_token", "private-production"); err != nil {
		t.Fatal(err)
	}
	got := v.Values("private-dev")
	want := map[string]string{"private_token": "private-replaced", "other": "private-other"}
	if !reflect.DeepEqual(got, want) || v.Values("private-prod")["private_token"] != "private-production" ||
		v.Values("unknown") != nil {
		t.Fatal("environment isolation or input cloning failed")
	}
	got["private_token"] = "caller-mutation"
	delete(got, "other")
	if !reflect.DeepEqual(v.Values("private-dev"), want) {
		t.Fatal("Values exposed internal map")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"private-dev", "private-prod", "private_token", "private-replaced", "private-other"} {
		if bytes.Contains(data, []byte(marker)) {
			t.Fatalf("plaintext on disk: %s", marker)
		}
	}
	v.Lock()
	reopened := openTestVault(t, path, keys)
	unlockTestVault(t, reopened)
	if !reflect.DeepEqual(reopened.Values("private-dev"), want) {
		t.Fatal("batch did not persist")
	}
	if err := reopened.Delete("private-dev", "private_token"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Delete("private-dev", "other"); err != nil {
		t.Fatal(err)
	}
	if reopened.Values("private-dev") != nil {
		t.Fatal("empty environment retained")
	}
	if err := reopened.Delete("private-prod", "private_token"); err != nil {
		t.Fatal(err)
	}
	if keys.removes != 0 || keys.creates != 1 {
		t.Fatal("deleting last secret removed or rotated key")
	}
	reopened.Lock()
	unlockTestVault(t, reopened)
	if reopened.Values("private-prod") != nil {
		t.Fatal("delete not persisted")
	}
}

func TestMutationsRequireUnlockedVault(t *testing.T) {
	v := openTestVault(t, testPath(t), &fakeKeyStore{})
	for _, mutate := range []func() error{
		func() error { return v.Set("dev", "token", "private-value") },
		func() error { return v.SetBatch("dev", map[string]string{"token": "private-value"}) },
		func() error { return v.Delete("dev", "token") },
	} {
		if err := mutate(); !errors.Is(err, ErrLocked) {
			t.Fatalf("locked mutation error: %v", err)
		}
	}
}

func TestSetBatchRejectsCandidateAtomically(t *testing.T) {
	for _, kind := range []string{
		"empty env", "long env", "control env", "invalid utf8 env", "empty name", "prefixed name",
		"bad name", "long name", "invalid utf8 value", "long value", "encoded size", "global count", "global size",
	} {
		t.Run(kind, func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			unlockTestVault(t, v)
			if err := v.Set("dev", "token", "private-original"); err != nil {
				t.Fatal(err)
			}
			env := "dev"
			input := map[string]string{"token": "private-replacement"}
			switch kind {
			case "empty env":
				env = ""
			case "long env":
				env = strings.Repeat("e", 1025)
			case "control env":
				env = "private-env\x7f"
			case "invalid utf8 env":
				env = "private-env\xff"
			case "empty name":
				input[""] = "private-invalid"
			case "prefixed name":
				input["secret.token"] = "private-invalid"
			case "bad name":
				input["private bad name"] = "private-invalid"
			case "long name":
				input[strings.Repeat("n", 257)] = "private-invalid"
			case "invalid utf8 value":
				input["invalid"] = "private-invalid\xff"
			case "long value":
				input["invalid"] = strings.Repeat("x", 65537)
			case "encoded size":
				for i := 0; i < 3; i++ {
					input[fmt.Sprintf("n%d", i)] = strings.Repeat("\x00", 60000)
				}
			case "global count":
				initial := make(map[string]string)
				for i := 0; i < 999; i++ {
					initial[fmt.Sprintf("n%d", i)] = "v"
				}
				if err := v.SetBatch("other", initial); err != nil {
					t.Fatal(err)
				}
				input["extra"] = "private-invalid"
			case "global size":
				initial := make(map[string]string)
				for i := 0; i < 16; i++ {
					initial[fmt.Sprintf("n%d", i)] = strings.Repeat("x", 60000)
				}
				if err := v.SetBatch("other", initial); err != nil {
					t.Fatal(err)
				}
				input["extra1"], input["extra2"] = strings.Repeat("x", 60000), strings.Repeat("x", 60000)
			}
			before, _ := os.ReadFile(path)
			want := v.Values("dev")
			if err := v.SetBatch(env, input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid candidate error: %v", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || !reflect.DeepEqual(v.Values("dev"), want) ||
				keys.creates != 1 || keys.loads != 0 || keys.removes != 0 {
				t.Fatal("failed batch changed file, memory or key store")
			}
		})
	}
}

func TestMutationWriteFailurePreservesFileAndMemory(t *testing.T) {
	for _, mutation := range []string{"set", "batch", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			path, keys := testPath(t), &fakeKeyStore{}
			v := openTestVault(t, path, keys)
			unlockTestVault(t, v)
			if err := v.Set("dev", "token", "private-original"); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
			var err error
			switch mutation {
			case "set":
				err = v.Set("dev", "token", "private-replacement")
			case "batch":
				err = v.SetBatch("dev", map[string]string{"token": "private-replacement", "other": "private-new"})
			case "delete":
				err = v.Delete("dev", "token")
			}
			if !errors.Is(err, ErrFile) {
				t.Fatalf("write failure not reported safely: %v", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) ||
				!reflect.DeepEqual(v.Values("dev"), map[string]string{"token": "private-original"}) {
				t.Fatal("failed write changed file or memory")
			}
		})
	}
}

func TestConcurrentMutationsSerializeWithoutLostUpdates(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	unlockTestVault(t, v)
	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- v.Set("dev", fmt.Sprintf("n%d", i), fmt.Sprintf("private-value-%d", i))
			copy := v.Values("dev")
			copy["caller"] = "mutation"
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(v.Values("dev")) != writers {
		t.Fatal("serialized writes lost values")
	}
	v.Lock()
	unlockTestVault(t, v)
	if len(v.Values("dev")) != writers {
		t.Fatal("serialized writes did not persist")
	}
}

func TestEveryWriteUsesFreshNonceAndNoOpPreservesFile(t *testing.T) {
	path, keys := testPath(t), &fakeKeyStore{}
	v := openTestVault(t, path, keys)
	unlockTestVault(t, v)
	seen := map[string]bool{string(readTestEnvelope(t, path).Nonce): true}
	for i := 0; i < 12; i++ {
		if err := v.Set("dev", "token", "private-value"); err != nil {
			t.Fatal(err)
		}
		nonce := string(readTestEnvelope(t, path).Nonce)
		if seen[nonce] {
			t.Fatal("nonce reused")
		}
		seen[nonce] = true
	}
	before, _ := os.ReadFile(path)
	if err := v.SetBatch("unknown", nil); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("unknown", "token"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || v.Values("unknown") != nil {
		t.Fatal("empty mutation published values")
	}
}
