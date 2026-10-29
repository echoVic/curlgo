package keychain

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type nativeAPI struct {
	itemAdd          func(uintptr, *uintptr) int32
	itemCopyMatching func(uintptr, *uintptr) int32
	itemDelete       func(uintptr) int32
	keychainOpen     func(string, *uintptr) int32

	dictionaryCreate func(uintptr, []uintptr, []uintptr, int, uintptr, uintptr) uintptr
	arrayCreate      func(uintptr, []uintptr, int, uintptr) uintptr
	stringCreate     func(uintptr, []byte, int, uint32, uint8) uintptr
	dataCreate       func(uintptr, []byte, int) uintptr
	dataLength       func(uintptr) int
	dataBytePtr      func(uintptr) *byte
	dataTypeID       func() uintptr
	getTypeID        func(uintptr) uintptr
	release          func(uintptr)

	class, genericPassword, service, account uintptr
	valueData, returnData                    uintptr
	matchLimit, matchLimitOne                uintptr
	useKeychain, searchList, booleanTrue     uintptr
	dictionaryKeys, dictionaryValues         uintptr
	arrayValues                              uintptr
}

var (
	nativeOnce sync.Once
	native     *nativeAPI
	nativeErr  error
)

func loadNative() (*nativeAPI, error) {
	nativeOnce.Do(func() { native, nativeErr = openNative() })
	return native, nativeErr
}

func openNative() (api *nativeAPI, err error) {
	defer func() {
		if recover() != nil {
			api, err = nil, ErrUnavailable
		}
	}()
	// Frameworks and their exported constant objects live for the process.
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, ErrUnavailable
	}
	sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, ErrUnavailable
	}
	api = &nativeAPI{}
	for _, binding := range []struct {
		handle uintptr
		name   string
		target any
	}{
		{sec, "SecItemAdd", &api.itemAdd},
		{sec, "SecItemCopyMatching", &api.itemCopyMatching},
		{sec, "SecItemDelete", &api.itemDelete},
		{sec, "SecKeychainOpen", &api.keychainOpen},
		{cf, "CFDictionaryCreate", &api.dictionaryCreate},
		{cf, "CFArrayCreate", &api.arrayCreate},
		{cf, "CFStringCreateWithBytes", &api.stringCreate},
		{cf, "CFDataCreate", &api.dataCreate},
		{cf, "CFDataGetLength", &api.dataLength},
		{cf, "CFDataGetBytePtr", &api.dataBytePtr},
		{cf, "CFDataGetTypeID", &api.dataTypeID},
		{cf, "CFGetTypeID", &api.getTypeID},
		{cf, "CFRelease", &api.release},
	} {
		symbol, err := purego.Dlsym(binding.handle, binding.name)
		if err != nil || symbol == 0 {
			return nil, ErrUnavailable
		}
		purego.RegisterFunc(binding.target, symbol)
	}
	for _, constant := range []struct {
		handle uintptr
		name   string
		target *uintptr
	}{
		{sec, "kSecClass", &api.class},
		{sec, "kSecClassGenericPassword", &api.genericPassword},
		{sec, "kSecAttrService", &api.service},
		{sec, "kSecAttrAccount", &api.account},
		{sec, "kSecValueData", &api.valueData},
		{sec, "kSecReturnData", &api.returnData},
		{sec, "kSecMatchLimit", &api.matchLimit},
		{sec, "kSecMatchLimitOne", &api.matchLimitOne},
		{sec, "kSecUseKeychain", &api.useKeychain},
		{sec, "kSecMatchSearchList", &api.searchList},
		{cf, "kCFBooleanTrue", &api.booleanTrue},
	} {
		symbol, err := purego.Dlsym(constant.handle, constant.name)
		if err != nil || symbol == 0 {
			return nil, ErrUnavailable
		}
		// Dlsym's native address points to a CF reference variable, not Go
		// memory. Reinterpret that address, then read the reference it stores.
		ptr := *(*unsafe.Pointer)(unsafe.Pointer(&symbol))
		*constant.target = *(*uintptr)(ptr)
		if *constant.target == 0 {
			return nil, ErrUnavailable
		}
	}
	for _, callbacks := range []struct {
		name   string
		target *uintptr
	}{
		{"kCFTypeDictionaryKeyCallBacks", &api.dictionaryKeys},
		{"kCFTypeDictionaryValueCallBacks", &api.dictionaryValues},
		{"kCFTypeArrayCallBacks", &api.arrayValues},
	} {
		// Callback exports are structs: pass the symbol address itself.
		symbol, err := purego.Dlsym(cf, callbacks.name)
		if err != nil || symbol == 0 {
			return nil, ErrUnavailable
		}
		*callbacks.target = symbol
	}
	return api, nil
}
