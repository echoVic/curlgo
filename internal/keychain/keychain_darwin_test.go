package keychain

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

func TestLifecycle(t *testing.T) {
	if os.Getenv("CURLGO_KEYCHAIN_TEST") != "1" {
		t.Skip("set CURLGO_KEYCHAIN_TEST=1 to access a random disposable keychain fixture")
	}
	id := hex.EncodeToString(randomBytes(t, 12))
	key := randomBytes(t, 32)
	store := New()
	ctx := context.Background()
	if err := store.Create(ctx, id, key); err != nil {
		t.Fatalf("create disposable fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Remove(context.Background(), id); err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("remove disposable fixture: %v", err)
		}
	})
	loaded, err := store.Load(ctx, id)
	if err != nil {
		t.Fatalf("load disposable fixture: %v", err)
	}
	if !bytes.Equal(loaded, key) {
		t.Fatal("fixture key did not round-trip")
	}
	loaded[0] ^= 1
	again, err := store.Load(ctx, id)
	if err != nil || !bytes.Equal(again, key) {
		t.Fatal("loaded key must be an independent Go copy")
	}
	if err := store.Create(ctx, id, randomBytes(t, 32)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("add-only duplicate: %v", err)
	}
	again, err = store.Load(ctx, id)
	if err != nil || !bytes.Equal(again, key) {
		t.Fatal("duplicate create must preserve the original key")
	}
	otherID := hex.EncodeToString(randomBytes(t, 12))
	if value, err := store.Load(ctx, otherID); value != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("exact lookup must not return another fixture: %v", err)
	}
	if err := store.Remove(ctx, id); err != nil {
		t.Fatalf("remove disposable fixture: %v", err)
	}
	if value, err := store.Load(ctx, id); value != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted fixture should be missing: %v", err)
	}
	if err := store.Remove(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second removal should be missing: %v", err)
	}
}

func randomBytes(t *testing.T, size int) []byte {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal("could not generate disposable fixture")
	}
	return value
}
