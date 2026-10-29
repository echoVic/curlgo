package main

import (
	"context"
	"io"
)

// interruptibleStdin is process-scoped: a blocked OS read can outlive
// cancellation only until main exits. Help never calls Read.
type interruptibleStdin struct {
	ctx    context.Context
	reader io.Reader
}

func (input interruptibleStdin) Read(p []byte) (int, error) {
	if err := input.ctx.Err(); err != nil {
		return 0, err
	}
	type result struct {
		n   int
		err error
	}
	ready := make(chan result, 1)
	buffer := make([]byte, len(p))
	go func() {
		n, err := input.reader.Read(buffer)
		ready <- result{n, err}
	}()
	select {
	case <-input.ctx.Done():
		return 0, input.ctx.Err()
	case read := <-ready:
		copy(p, buffer[:read.n])
		return read.n, read.err
	}
}
