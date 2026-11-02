package browse

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/zmerlynn/seestar-copy/internal/remote"
)

// logBuffer collects log lines from any goroutine.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// count is how many lines hold every one of parts.
func (l *logBuffer) count(parts ...string) int {
	n := 0
	for line := range strings.Lines(l.String()) {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all {
			n++
		}
	}
	return n
}

func newLog() (*slog.Logger, *logBuffer) {
	var b logBuffer
	return slog.New(slog.NewTextHandler(&b, nil)), &b
}

// spy is a Session over a Fake that counts its calls.
type spy struct {
	*remote.Fake
	ctx  context.Context // the dial's
	done chan struct{}
	once sync.Once

	mu    sync.Mutex
	calls map[string]int
}

func (s *spy) count(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[name]++
}

func (s *spy) n(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[name]
}

// closed reports whether Close was called.
func (s *spy) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *spy) Close() error {
	s.count("Close")
	s.once.Do(func() { close(s.done) })
	return s.Fake.Close()
}

// dialer makes a new spy over fsys for each dial and counts them. With gate
// set, each dial waits for it to close, ignoring its context; with err set,
// each dial fails.
type dialer struct {
	fsys  fs.FS
	gate  chan struct{}
	err   error
	setup func(*spy) // adjusts each new spy

	mu       sync.Mutex
	sessions []*spy
}

func (d *dialer) dial(ctx context.Context) (remote.Session, error) {
	d.mu.Lock()
	s := &spy{Fake: remote.NewFake(d.fsys), ctx: ctx, done: make(chan struct{}), calls: map[string]int{}}
	if d.setup != nil {
		d.setup(s)
	}
	d.sessions = append(d.sessions, s)
	d.mu.Unlock()
	if d.gate != nil {
		<-d.gate
	}
	if d.err != nil {
		return nil, d.err
	}
	return s, nil
}

// dials is how many dials have started.
func (d *dialer) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sessions)
}

// session is the i-th dialed spy.
func (d *dialer) session(i int) *spy {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[i]
}

// leases is the current session's lease count, or -1 without one.
func (p *Sessions) leases() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return -1
	}
	return p.cur.leases
}

// mustGet takes a lease or fails the test.
func mustGet(t *testing.T, p *Sessions, ctx context.Context) *Lease {
	t.Helper()
	l, err := p.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return l
}
