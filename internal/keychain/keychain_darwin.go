package keychain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

type operation uint8

const (
	opCreate operation = iota
	opLoad
	opRemove
)

func platformCreate(ctx context.Context, id string, key []byte) error {
	api, err := loadNative()
	if err != nil {
		return err
	}
	query, err := api.query(ctx, id, opCreate, key)
	if err != nil {
		return err
	}
	defer api.release(query)
	if err := ctx.Err(); err != nil {
		return err
	}
	return createResult(ctx, api.itemAdd(query, nil))
}

func platformLoad(ctx context.Context, id string) ([]byte, error) {
	api, err := loadNative()
	if err != nil {
		return nil, err
	}
	return api.load(ctx, id)
}

func platformRemove(ctx context.Context, id string) error {
	api, err := loadNative()
	if err != nil {
		return err
	}
	query, err := api.query(ctx, id, opRemove, nil)
	if err != nil {
		return err
	}
	defer api.release(query)
	if err := ctx.Err(); err != nil {
		return err
	}
	status := api.itemDelete(query)
	if err := statusError(status); err != nil {
		return err
	}
	return ctx.Err()
}

func createResult(ctx context.Context, status int32) error {
	cancellation := ctx.Err()
	switch status {
	case 0:
		// Creation is committed. The caller can now safely roll back its item.
		return nil
	case -25299:
		// Cancellation must not turn a preexisting item into a rollback target.
		return ErrDuplicate
	}
	if cancellation != nil {
		return cancellation
	}
	return statusError(status)
}

func statusError(status int32) error {
	switch status {
	case 0:
		return nil
	case -25299:
		return ErrDuplicate
	case -25300:
		return ErrNotFound
	case -128, -25293, -25308:
		return ErrDenied
	default:
		return ErrUnavailable
	}
}

func (a *nativeAPI) load(ctx context.Context, id string) ([]byte, error) {
	query, err := a.query(ctx, id, opLoad, nil)
	if err != nil {
		return nil, err
	}
	defer a.release(query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result uintptr
	status := a.itemCopyMatching(query, &result)
	if result != 0 {
		defer a.release(result)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := statusError(status); err != nil {
		return nil, err
	}
	if result == 0 || a.getTypeID(result) != a.dataTypeID() || a.dataLength(result) != 32 {
		return nil, ErrInvalid
	}
	ptr := a.dataBytePtr(result)
	if ptr == nil {
		return nil, ErrInvalid
	}
	key := make([]byte, 32)
	copy(key, unsafe.Slice(ptr, 32))
	if err := ctx.Err(); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

func (a *nativeAPI) query(ctx context.Context, id string, op operation, key []byte) (uintptr, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, ErrUnavailable
	}
	var owned []uintptr
	defer func() {
		for _, ref := range owned {
			a.release(ref)
		}
	}()
	var login uintptr
	status := a.keychainOpen(filepath.Join(home, "Library", "Keychains", "login.keychain-db"), &login)
	if login != 0 {
		owned = append(owned, login)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := statusError(status); err != nil {
		return 0, err
	}
	if login == 0 {
		return 0, ErrUnavailable
	}
	service := a.stringValue("com.echovic.curlgo.vault." + id)
	if service == 0 {
		return 0, ErrUnavailable
	}
	owned = append(owned, service)
	account := a.stringValue("master-key")
	if account == 0 {
		return 0, ErrUnavailable
	}
	owned = append(owned, account)
	keys := []uintptr{a.class, a.service, a.account}
	values := []uintptr{a.genericPassword, service, account}
	if op == opCreate {
		data := a.dataCreate(0, key, len(key))
		runtime.KeepAlive(key)
		if data == 0 {
			return 0, ErrUnavailable
		}
		owned = append(owned, data)
		keys = append(keys, a.useKeychain, a.valueData)
		values = append(values, login, data)
	} else {
		items := []uintptr{login}
		list := a.arrayCreate(0, items, len(items), a.arrayValues)
		runtime.KeepAlive(items)
		if list == 0 {
			return 0, ErrUnavailable
		}
		owned = append(owned, list)
		keys = append(keys, a.searchList)
		values = append(values, list)
		if op == opLoad {
			keys = append(keys, a.returnData, a.matchLimit)
			values = append(values, a.booleanTrue, a.matchLimitOne)
		}
	}
	query := a.dictionaryCreate(0, keys, values, len(keys), a.dictionaryKeys, a.dictionaryValues)
	runtime.KeepAlive(keys)
	runtime.KeepAlive(values)
	if query == 0 {
		return 0, ErrUnavailable
	}
	// CF callbacks retain every value; release local ownership after creation.
	return query, nil
}

func (a *nativeAPI) stringValue(value string) uintptr {
	data := []byte(value)
	ref := a.stringCreate(0, data, len(data), 0x08000100, 0)
	runtime.KeepAlive(data)
	return ref
}
