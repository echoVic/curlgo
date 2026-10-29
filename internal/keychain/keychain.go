// Package keychain stores vault master keys in the macOS login keychain.
package keychain

import "context"

// Error is a fixed, sanitized keychain failure. Native status codes, paths,
// identifiers, and key material are never included.
type Error struct {
	message string
}

func (e *Error) Error() string { return e.message }

var (
	ErrUnsupported = &Error{"keychain: unsupported platform"}
	ErrInvalid     = &Error{"keychain: invalid vault identifier or master key"}
	ErrNotFound    = &Error{"keychain: master key not found"}
	ErrDuplicate   = &Error{"keychain: master key already exists"}
	ErrDenied      = &Error{"keychain: access denied or canceled"}
	ErrUnavailable = &Error{"keychain: unavailable"}
)

// Store is ready to use without initialization. Its methods may block in native
// authentication UI; context cancellation cannot interrupt a native call.
type Store struct{}

// New returns a store without opening frameworks or accessing the keychain.
func New() *Store { return &Store{} }

// Create adds a 32-byte key without overwriting an existing item.
// A successful native add always returns nil, including cancellation during
// that call. The caller must then check ctx.Err() and roll back with Remove
// using a fresh context if needed. A non-nil error never signifies creation.
func (s *Store) Create(ctx context.Context, id string, key []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validID(id) || len(key) != 32 {
		return ErrInvalid
	}
	return platformCreate(ctx, id, key)
}

// Load copies exactly one 32-byte master key into Go-owned memory.
func (s *Store) Load(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, ErrInvalid
	}
	return platformLoad(ctx, id)
}

// Remove deletes only the specified vault's master key. It is intended for
// rollback of failed vault initialization, not normal vault locking.
func (s *Store) Remove(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validID(id) {
		return ErrInvalid
	}
	return platformRemove(ctx, id)
}

func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
