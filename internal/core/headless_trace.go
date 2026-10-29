package core

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

type httpTraceState struct {
	mu      sync.Mutex
	start   time.Time
	now     func() time.Time
	timing  HTTPTiming
	pending map[string][]time.Time
	frozen  bool
}

func newHTTPTrace(start time.Time, now func() time.Time) (*httpTraceState, *httptrace.ClientTrace) {
	state := &httpTraceState{
		start:   start,
		now:     now,
		pending: make(map[string][]time.Time),
	}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			state.begin("dns")
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			state.end("dns", &state.timing.DNS)
		},
		TLSHandshakeStart: func() {
			state.begin("tls")
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			state.end("tls", &state.timing.TLS)
		},
		ConnectStart: func(network, address string) {
			state.begin("connect:" + network + "\x00" + address)
		},
		ConnectDone: func(network, address string, _ error) {
			state.end("connect:"+network+"\x00"+address, &state.timing.Connect)
		},
		GotFirstResponseByte: func() {
			state.mu.Lock()
			defer state.mu.Unlock()
			if !state.frozen {
				state.timing.TTFB = state.now().Sub(state.start)
			}
		},
	}
	return state, trace
}

func (s *httpTraceState) begin(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		return
	}
	s.pending[key] = append(s.pending[key], s.now())
}

func (s *httpTraceState) end(key string, duration *time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		return
	}
	starts := s.pending[key]
	if len(starts) == 0 {
		return
	}
	*duration += s.now().Sub(starts[0])
	if len(starts) == 1 {
		delete(s.pending, key)
	} else {
		s.pending[key] = starts[1:]
	}
}

func (s *httpTraceState) finish() HTTPTiming {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.frozen {
		s.timing.Total = s.now().Sub(s.start)
		s.frozen = true
	}
	return s.timing
}
