// Package vault stores environment-scoped secrets in an authenticated private file.
package vault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/echoVic/curlgo/internal/core"
)

var (
	ErrFile           = errors.New("vault: private file unavailable")
	ErrCorrupt        = errors.New("vault: invalid encrypted data")
	ErrKeyUnavailable = errors.New("vault: key unavailable")
	ErrCrypto         = errors.New("vault: encryption unavailable")
	ErrCanceled       = errors.New("vault: unlock canceled")
	ErrBusy           = errors.New("vault: unlock already in progress")
	ErrCleanup        = errors.New("vault: initialization cleanup failed")
	ErrInvalid        = errors.New("vault: invalid secret values")
	ErrLocked         = errors.New("vault: locked")
)

// KeyStore must create keys without overwriting an existing item. Create must
// return nil after adding an item, even if cancellation follows the native call;
// an error must mean no item was created. Load returns an owned byte slice.
type KeyStore interface {
	Create(ctx context.Context, id string, key []byte) error
	Load(ctx context.Context, id string) ([]byte, error)
	Remove(ctx context.Context, id string) error
}

type Vault struct {
	mu         sync.Mutex
	path       string
	keys       KeyStore
	envelope   *envelope
	key        []byte
	values     map[string]map[string]string
	generation uint64
	unlocking  bool
}

// Open validates the file without creating files or accessing the key store.
func Open(path string, keys KeyStore) (*Vault, error) {
	e, err := readEnvelope(path)
	if err != nil {
		return nil, err
	}
	return &Vault{path: path, keys: keys, envelope: e}, nil
}

// Unlock is the only operation that accesses keys. A missing file initializes a
// vault; a missing key or invalid existing file never does.
func (v *Vault) Unlock(ctx context.Context) error {
	v.mu.Lock()
	if ctx.Err() != nil {
		v.mu.Unlock()
		return ErrCanceled
	}
	if len(v.key) != 0 {
		v.mu.Unlock()
		return nil
	}
	if v.unlocking {
		v.mu.Unlock()
		return ErrBusy
	}
	if v.keys == nil {
		v.mu.Unlock()
		return ErrKeyUnavailable
	}
	v.unlocking = true
	generation, existed := v.generation, v.envelope != nil
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.unlocking = false
		v.mu.Unlock()
	}()

	e, err := readEnvelope(v.path)
	if err != nil {
		return err
	}
	if e == nil {
		if existed {
			return ErrFile
		}
		return v.initialize(ctx, generation)
	}
	key, err := v.keys.Load(ctx, e.ID)
	defer clear(key)
	if ctx.Err() != nil {
		return ErrCanceled
	}
	if err != nil || len(key) != 32 {
		return ErrKeyUnavailable
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, e.Nonce, e.Ciphertext, associatedData(e))
	if err != nil {
		return ErrCorrupt
	}
	defer clear(plain)
	values, err := decodePlaintext(plain)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if ctx.Err() != nil || generation != v.generation {
		return ErrCanceled
	}
	if err := unchangedEnvelope(v.path, e); err != nil {
		return err
	}
	v.key, v.values, v.envelope = bytes.Clone(key), values, e
	return nil
}

func (v *Vault) initialize(ctx context.Context, generation uint64) (err error) {
	key, id := make([]byte, 32), make([]byte, 12)
	defer clear(key)
	if _, err = rand.Read(key); err != nil {
		return ErrCrypto
	}
	if _, err = rand.Read(id); err != nil {
		return ErrCrypto
	}
	e := &envelope{Version: 1, ID: hex.EncodeToString(id)}
	if ctx.Err() != nil {
		return ErrCanceled
	}
	if err = v.keys.Create(ctx, e.ID, key); err != nil {
		return ErrKeyUnavailable
	}
	defer func() {
		if err != nil && v.keys.Remove(context.WithoutCancel(ctx), e.ID) != nil {
			err = ErrCleanup
		}
	}()
	if ctx.Err() != nil {
		return ErrCanceled
	}
	e, data, err := encrypt(key, e.ID, []byte("{}"))
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	check := func() error {
		if ctx.Err() != nil || generation != v.generation {
			return ErrCanceled
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	if err = writeEnvelope(v.path, data, true, check); err != nil {
		return err
	}
	// Publication is the commit point; later cancellation must not orphan the file.
	v.key, v.values, v.envelope = bytes.Clone(key), make(map[string]map[string]string), e
	return nil
}

// Lock clears owned key bytes and drops values. Go strings and caller copies
// cannot be securely erased.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.generation++
	clear(v.key)
	v.key, v.values = nil, nil
}

func (v *Vault) Unlocked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.key) != 0
}

// Values returns a detached environment map, or nil when locked or unknown.
func (v *Vault) Values(environment string) map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.key) == 0 || v.values[environment] == nil {
		return nil
	}
	values := make(map[string]string, len(v.values[environment]))
	for name, value := range v.values[environment] {
		values[name] = value
	}
	return values
}

func (v *Vault) Set(environment, name, value string) error {
	return v.SetBatch(environment, map[string]string{name: value})
}

// SetBatch merges values only after the entire candidate is atomically published.
func (v *Vault) SetBatch(environment string, values map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.key) == 0 {
		return ErrLocked
	}
	if !validEnvironment(environment) || core.ValidateSecretValues(values) != nil {
		return ErrInvalid
	}
	if len(values) == 0 {
		return nil
	}
	candidate := cloneValues(v.values)
	if candidate[environment] == nil {
		candidate[environment] = make(map[string]string)
	}
	for name, value := range values {
		candidate[environment][name] = value
	}
	return v.commit(candidate)
}

// Delete never removes the vault key, including when the last value is deleted.
func (v *Vault) Delete(environment, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.key) == 0 {
		return ErrLocked
	}
	if !validEnvironment(environment) || core.ValidateSecretValues(map[string]string{name: ""}) != nil {
		return ErrInvalid
	}
	if _, exists := v.values[environment][name]; !exists {
		return nil
	}
	candidate := cloneValues(v.values)
	delete(candidate[environment], name)
	if len(candidate[environment]) == 0 {
		delete(candidate, environment)
	}
	return v.commit(candidate)
}

func cloneValues(values map[string]map[string]string) map[string]map[string]string {
	copy := make(map[string]map[string]string, len(values))
	for environment, secrets := range values {
		copy[environment] = make(map[string]string, len(secrets))
		for name, value := range secrets {
			copy[environment][name] = value
		}
	}
	return copy
}

// commit requires v.mu; disk publication precedes replacing the live map.
func (v *Vault) commit(values map[string]map[string]string) error {
	if err := validateValues(values); err != nil {
		return err
	}
	plain, err := json.Marshal(values)
	defer clear(plain)
	if err != nil || len(plain) > core.MaxInput {
		return ErrInvalid
	}
	e, data, err := encrypt(v.key, v.envelope.ID, plain)
	if err != nil {
		return err
	}
	if err := writeEnvelope(v.path, data, false, func() error {
		return unchangedEnvelope(v.path, v.envelope)
	}); err != nil {
		return err
	}
	v.envelope, v.values = e, values
	return nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrKeyUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCrypto
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCrypto
	}
	return gcm, nil
}

func associatedData(e *envelope) []byte {
	return []byte("curlgo.secret-vault\x00v1\x00" + e.ID)
}

func encrypt(key []byte, id string, plain []byte) (*envelope, []byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	e := &envelope{Version: 1, ID: id, Nonce: make([]byte, gcm.NonceSize())}
	if _, err = rand.Read(e.Nonce); err != nil {
		return nil, nil, ErrCrypto
	}
	e.Ciphertext = gcm.Seal(nil, e.Nonce, plain, associatedData(e))
	data, err := json.Marshal(e)
	if err != nil || len(data) > maxEnvelope {
		return nil, nil, ErrCrypto
	}
	return e, data, nil
}

func validEnvironment(name string) bool {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateValues(values map[string]map[string]string) error {
	if len(values) > core.MaxSecrets {
		return ErrInvalid
	}
	count := 0
	for environment, secrets := range values {
		if !validEnvironment(environment) || secrets == nil || core.ValidateSecretValues(secrets) != nil {
			return ErrInvalid
		}
		count += len(secrets)
		if count > core.MaxSecrets {
			return ErrInvalid
		}
	}
	return nil
}
