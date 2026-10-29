package core

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"
)

func TestHeadlessTracePhases(t *testing.T) {
	start := time.Unix(0, 0)
	clock := start
	clockReads := 0
	state, trace := newHTTPTrace(start, func() time.Time {
		clockReads++
		return clock
	})

	clock = start.Add(time.Millisecond)
	trace.DNSStart(httptrace.DNSStartInfo{})
	clock = start.Add(3 * time.Millisecond)
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", "a")
	clock = start.Add(4 * time.Millisecond)
	trace.ConnectStart("tcp", "b")
	clock = start.Add(6 * time.Millisecond)
	trace.ConnectDone("tcp", "b", nil)
	clock = start.Add(8 * time.Millisecond)
	trace.ConnectDone("tcp", "a", nil)
	trace.TLSHandshakeStart()
	clock = start.Add(11 * time.Millisecond)
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	trace.GotFirstResponseByte()
	clock = start.Add(15 * time.Millisecond)
	trace.GotFirstResponseByte()
	clock = start.Add(19 * time.Millisecond)

	got := state.finish()
	want := HTTPTiming{
		DNS:     2 * time.Millisecond,
		Connect: 7 * time.Millisecond,
		TLS:     3 * time.Millisecond,
		TTFB:    15 * time.Millisecond,
		Total:   19 * time.Millisecond,
	}
	if got != want {
		t.Fatalf("timing = %+v, want %+v", got, want)
	}

	readsAtFinish := clockReads
	clock = start.Add(time.Second)
	trace.GotFirstResponseByte()
	trace.DNSStart(httptrace.DNSStartInfo{})
	trace.ConnectStart("tcp", "a")
	trace.TLSHandshakeStart()
	clock = start.Add(2 * time.Second)
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectDone("tcp", "a", nil)
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	if frozen := state.finish(); frozen != got {
		t.Fatalf("frozen timing = %+v, want %+v", frozen, got)
	}
	if clockReads != readsAtFinish {
		t.Fatalf("frozen callbacks read the clock: %d reads, want %d", clockReads, readsAtFinish)
	}

	got.DNS = time.Hour
	if snapshot := state.finish(); snapshot != want {
		t.Fatalf("returned timing changed state: %+v, want %+v", snapshot, want)
	}
}

func TestHeadlessTraceConcurrent(t *testing.T) {
	state, trace := newHTTPTrace(time.Now(), time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			trace.ConnectStart("tcp", "same-address")
			trace.ConnectDone("tcp", "same-address", nil)
			trace.GotFirstResponseByte()
			state.finish()
		})
	}
	wg.Wait()

	got := state.finish()
	if got.Total < 0 || got.Connect < 0 || got.TTFB < 0 {
		t.Fatalf("negative timing: %+v", got)
	}
}

func TestHeadlessTraceNoFirstByte(t *testing.T) {
	start := time.Unix(0, 0)
	state, _ := newHTTPTrace(start, func() time.Time { return start.Add(time.Second) })
	want := HTTPTiming{Total: time.Second}
	if got := state.finish(); got != want {
		t.Fatalf("timing = %+v, want %+v", got, want)
	}
}

func TestHeadlessTracePendingPhases(t *testing.T) {
	tests := []struct {
		name  string
		begin func(*httptrace.ClientTrace)
		end   func(*httptrace.ClientTrace)
		want  HTTPTiming
	}{
		{
			name:  "dns",
			begin: func(trace *httptrace.ClientTrace) { trace.DNSStart(httptrace.DNSStartInfo{}) },
			end:   func(trace *httptrace.ClientTrace) { trace.DNSDone(httptrace.DNSDoneInfo{}) },
			want:  HTTPTiming{DNS: 5 * time.Millisecond},
		},
		{
			name:  "tls",
			begin: func(trace *httptrace.ClientTrace) { trace.TLSHandshakeStart() },
			end:   func(trace *httptrace.ClientTrace) { trace.TLSHandshakeDone(tls.ConnectionState{}, nil) },
			want:  HTTPTiming{TLS: 5 * time.Millisecond},
		},
		{
			name:  "connect",
			begin: func(trace *httptrace.ClientTrace) { trace.ConnectStart("tcp", "same-address") },
			end:   func(trace *httptrace.ClientTrace) { trace.ConnectDone("tcp", "same-address", nil) },
			want:  HTTPTiming{Connect: 5 * time.Millisecond},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Unix(0, 0)
			clock := start
			state, trace := newHTTPTrace(start, func() time.Time { return clock })
			tt.end(trace)
			clock = start.Add(2 * time.Millisecond)
			tt.begin(trace)
			clock = start.Add(4 * time.Millisecond)
			tt.begin(trace)
			clock = start.Add(7 * time.Millisecond)
			tt.end(trace)
			if got := state.timing; got != tt.want {
				t.Fatalf("first completion = %+v, want %+v", got, tt.want)
			}
			clock = start.Add(9 * time.Millisecond)
			tt.end(trace)
			clock = start.Add(11 * time.Millisecond)
			tt.end(trace)
			clock = start.Add(12 * time.Millisecond)
			tt.begin(trace)
			clock = start.Add(13 * time.Millisecond)
			want := HTTPTiming{
				DNS:     2 * tt.want.DNS,
				Connect: 2 * tt.want.Connect,
				TLS:     2 * tt.want.TLS,
				Total:   13 * time.Millisecond,
			}
			if got := state.finish(); got != want {
				t.Fatalf("completed timing = %+v, want %+v", got, want)
			}
		})
	}

	t.Run("connect keys", func(t *testing.T) {
		start := time.Unix(0, 0)
		clock := start.Add(time.Millisecond)
		state, trace := newHTTPTrace(start, func() time.Time { return clock })
		trace.ConnectStart("tcp", "a")
		clock = start.Add(2 * time.Millisecond)
		trace.ConnectDone("udp", "a", nil)
		trace.ConnectDone("tcp", "b", nil)
		trace.ConnectStart("tc", "pa")
		clock = start.Add(5 * time.Millisecond)
		trace.ConnectDone("tc", "pa", nil)
		if got := state.timing.Connect; got != 3*time.Millisecond {
			t.Fatalf("connect key collision: duration = %v, want 3ms", got)
		}
		clock = start.Add(7 * time.Millisecond)
		trace.ConnectDone("tcp", "a", nil)
		clock = start.Add(9 * time.Millisecond)
		want := HTTPTiming{Connect: 9 * time.Millisecond, Total: 9 * time.Millisecond}
		if got := state.finish(); got != want {
			t.Fatalf("connect key timing = %+v, want %+v", got, want)
		}
	})
}
