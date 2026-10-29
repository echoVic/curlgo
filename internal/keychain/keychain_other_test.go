//go:build !darwin

package keychain

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatform(t *testing.T) {
	store := New()
	if err := store.Create(context.Background(), testID, make([]byte, 32)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Create: expected unsupported platform, got %v", err)
	}
	if value, err := store.Load(context.Background(), testID); !errors.Is(err, ErrUnsupported) || value != nil {
		t.Fatalf("Load: expected unsupported platform with no data, got %v", err)
	}
	if err := store.Remove(context.Background(), testID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Remove: expected unsupported platform, got %v", err)
	}
}
