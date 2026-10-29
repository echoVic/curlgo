package keychain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const testID = "0123456789abcdef01234567"

func TestRejectInvalidIDs(t *testing.T) {
	store := New()
	for _, id := range []string{"", "0123456789abcdef0123456", testID + "8", strings.ToUpper(testID), "../private", testID[:23] + "g", testID[:23] + "\x00"} {
		t.Run(id, func(t *testing.T) {
			if err := store.Create(context.Background(), id, make([]byte, 32)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Create: expected invalid input, got %v", err)
			}
			if value, err := store.Load(context.Background(), id); !errors.Is(err, ErrInvalid) || value != nil {
				t.Fatalf("Load: expected invalid input with no data, got %v", err)
			}
			if err := store.Remove(context.Background(), id); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Remove: expected invalid input, got %v", err)
			}
		})
	}
}

func TestRejectInvalidKeyLength(t *testing.T) {
	store := New()
	for _, length := range []int{0, 1, 31, 33, 64} {
		if err := store.Create(context.Background(), testID, make([]byte, length)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("length %d: expected invalid input, got %v", length, err)
		}
	}
}

func TestCanceledContextDoesNotAccessKeychain(t *testing.T) {
	var store Store
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Create(ctx, testID, make([]byte, 32)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create: expected cancellation, got %v", err)
	}
	if value, err := store.Load(ctx, testID); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatalf("Load: expected cancellation with no data, got %v", err)
	}
	if err := store.Remove(ctx, testID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Remove: expected cancellation, got %v", err)
	}
}

func TestErrorsAreTypedAndSanitized(t *testing.T) {
	for _, err := range []error{ErrUnsupported, ErrInvalid, ErrNotFound, ErrDuplicate, ErrDenied, ErrUnavailable} {
		var typed *Error
		if !errors.As(err, &typed) {
			t.Fatal("expected typed keychain error")
		}
		if !strings.HasPrefix(err.Error(), "keychain: ") || strings.Contains(err.Error(), testID) {
			t.Fatal("expected fixed sanitized error")
		}
	}
}
