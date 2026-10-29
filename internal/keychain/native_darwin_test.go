package keychain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNativeErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		status int32
		want   error
	}{
		{0, nil},
		{-25299, ErrDuplicate},
		{-25300, ErrNotFound},
		{-128, ErrDenied},
		{-25293, ErrDenied},
		{-25308, ErrDenied},
		{-25291, ErrUnavailable},
		{-123456, ErrUnavailable},
	} {
		if got := statusError(tc.status); !errors.Is(got, tc.want) {
			t.Fatalf("expected sanitized %v, got %v", tc.want, got)
		}
	}
}

func TestCreateCancellationPreservesCommitOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := createResult(ctx, 0); err != nil {
		t.Fatalf("confirmed creation must remain detectable for rollback: %v", err)
	}
	if err := createResult(ctx, -25299); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate must never be reported as created or hidden by cancellation: %v", err)
	}
	if err := createResult(ctx, -25308); !errors.Is(err, context.Canceled) {
		t.Fatalf("failed creation may report cancellation: %v", err)
	}
}

func TestQueriesAreExactAndReleaseOwnedObjects(t *testing.T) {
	for _, op := range []operation{opCreate, opLoad, opRemove} {
		t.Run([]string{"create", "load", "remove"}[op], func(t *testing.T) {
			api, refs := fakeAPI(t)
			query, err := api.query(context.Background(), testID, op, make([]byte, 32))
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			attrs := refs[query].attrs
			if attrs[api.class] != api.genericPassword {
				t.Fatal("query must select generic passwords")
			}
			if refs[attrs[api.service]].text != "com.echovic.curlgo.vault."+testID || refs[attrs[api.account]].text != "master-key" {
				t.Fatal("query must match the exact service and account")
			}
			switch op {
			case opCreate:
				if attrs[api.useKeychain] == 0 || len(refs[attrs[api.valueData]].data) != 32 || len(attrs) != 5 {
					t.Fatal("create must add only a key in the login keychain")
				}
			case opLoad:
				if len(attrs) != 6 || attrs[api.returnData] != api.booleanTrue || attrs[api.matchLimit] != api.matchLimitOne {
					t.Fatal("load must request exactly one data result")
				}
				fallthrough
			case opRemove:
				list := refs[attrs[api.searchList]]
				if len(list.items) != 1 || refs[list.items[0]].text != "login" {
					t.Fatal("search must be restricted to the login keychain")
				}
				if op == opRemove && len(attrs) != 4 {
					t.Fatal("remove must contain only exact-match search attributes")
				}
			}
			api.release(query)
			for _, ref := range refs {
				if ref.count != 0 {
					t.Fatal("query leaked an owned CoreFoundation reference")
				}
			}
		})
	}
}

func TestLoadCopiesDataBeforeRelease(t *testing.T) {
	api, refs := fakeAPI(t)
	fixture := make([]byte, 32)
	for i := range fixture {
		fixture[i] = byte(i + 1)
	}
	result := addFakeRef(refs, &fakeRef{count: 1, data: fixture})
	api.itemCopyMatching = func(_ uintptr, out *uintptr) int32 {
		*out = result
		return 0
	}
	key, err := api.load(context.Background(), testID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for i, value := range key {
		if value != byte(i+1) {
			t.Fatal("load returned released native memory instead of a copy")
		}
	}
	if refs[result].count != 0 {
		t.Fatal("copy-matching result was not released")
	}
}

func TestLoadCancellationReleasesResultWithoutReturningData(t *testing.T) {
	api, refs := fakeAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := addFakeRef(refs, &fakeRef{count: 1, data: make([]byte, 32)})
	api.itemCopyMatching = func(_ uintptr, out *uintptr) int32 {
		*out = result
		cancel()
		return 0
	}
	key, err := api.load(ctx, testID)
	if key != nil || !errors.Is(err, context.Canceled) || refs[result].count != 0 {
		t.Fatal("canceled load must discard data and release the retained result")
	}
}

func TestLoadRejectsInvalidNativeData(t *testing.T) {
	for _, length := range []int{0, 31, 33} {
		api, refs := fakeAPI(t)
		result := addFakeRef(refs, &fakeRef{count: 1, data: make([]byte, length)})
		api.itemCopyMatching = func(_ uintptr, out *uintptr) int32 {
			*out = result
			return 0
		}
		key, err := api.load(context.Background(), testID)
		if key != nil || !errors.Is(err, ErrInvalid) || refs[result].count != 0 {
			t.Fatal("invalid native data must be rejected and released")
		}
	}
}

type fakeRef struct {
	count int
	text  string
	data  []byte
	attrs map[uintptr]uintptr
	items []uintptr
}

func addFakeRef(refs map[uintptr]*fakeRef, ref *fakeRef) uintptr {
	id := uintptr(100 + len(refs))
	refs[id] = ref
	return id
}

func fakeAPI(t *testing.T) (*nativeAPI, map[uintptr]*fakeRef) {
	t.Helper()
	refs := make(map[uintptr]*fakeRef)
	api := &nativeAPI{
		class: 1, genericPassword: 2, service: 3, account: 4,
		valueData: 5, returnData: 6, matchLimit: 7, matchLimitOne: 8,
		useKeychain: 9, searchList: 10, booleanTrue: 11,
		dictionaryKeys: 12, dictionaryValues: 13, arrayValues: 14,
	}
	api.keychainOpen = func(path string, out *uintptr) int32 {
		if !strings.HasSuffix(path, "/Library/Keychains/login.keychain-db") {
			t.Fatal("must open only the login keychain")
		}
		*out = addFakeRef(refs, &fakeRef{count: 1, text: "login"})
		return 0
	}
	api.stringCreate = func(_ uintptr, data []byte, length int, encoding uint32, external uint8) uintptr {
		if length != len(data) || encoding != 0x08000100 || external != 0 {
			t.Fatal("invalid CF string ABI arguments")
		}
		return addFakeRef(refs, &fakeRef{count: 1, text: string(data)})
	}
	api.dataCreate = func(_ uintptr, data []byte, length int) uintptr {
		return addFakeRef(refs, &fakeRef{count: 1, data: append([]byte(nil), data[:length]...)})
	}
	api.arrayCreate = func(_ uintptr, items []uintptr, length int, callbacks uintptr) uintptr {
		if callbacks != api.arrayValues {
			t.Fatal("array callback address must be passed without dereferencing")
		}
		for _, item := range items[:length] {
			refs[item].count++
		}
		return addFakeRef(refs, &fakeRef{count: 1, items: append([]uintptr(nil), items[:length]...)})
	}
	api.dictionaryCreate = func(_ uintptr, keys, values []uintptr, length int, keyCallbacks, valueCallbacks uintptr) uintptr {
		if keyCallbacks != api.dictionaryKeys || valueCallbacks != api.dictionaryValues {
			t.Fatal("dictionary callback addresses must be passed without dereferencing")
		}
		attrs := make(map[uintptr]uintptr)
		for i := range length {
			attrs[keys[i]] = values[i]
			if ref := refs[values[i]]; ref != nil {
				ref.count++
			}
		}
		return addFakeRef(refs, &fakeRef{count: 1, attrs: attrs})
	}
	api.release = func(id uintptr) {
		ref := refs[id]
		if ref == nil || ref.count <= 0 {
			t.Fatal("release of an unowned reference")
		}
		ref.count--
		if ref.count == 0 {
			clear(ref.data)
			for _, value := range ref.attrs {
				if refs[value] != nil {
					api.release(value)
				}
			}
			for _, item := range ref.items {
				api.release(item)
			}
		}
	}
	api.getTypeID = func(uintptr) uintptr { return 42 }
	api.dataTypeID = func() uintptr { return 42 }
	api.dataLength = func(id uintptr) int { return len(refs[id].data) }
	api.dataBytePtr = func(id uintptr) *byte { return &refs[id].data[0] }
	return api, refs
}
