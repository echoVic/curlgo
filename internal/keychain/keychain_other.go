//go:build !darwin

package keychain

import "context"

func platformCreate(context.Context, string, []byte) error { return ErrUnsupported }

func platformLoad(context.Context, string) ([]byte, error) { return nil, ErrUnsupported }

func platformRemove(context.Context, string) error { return ErrUnsupported }
