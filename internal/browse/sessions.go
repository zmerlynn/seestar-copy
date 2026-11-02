// Package browse serves a read-only web page for looking through the
// telescope's share: the HTTP handler, its embedded page, and the pool that
// keeps one SMB session for all requests.
package browse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zmerlynn/seestar-copy/internal/remote"
)

// Fixed times of the session pool. None is a flag.
const (
	dialTimeout = 30 * time.Second // a dial and mount that takes longer fails
	idleTimeout = 30 * time.Second // an unused session logs off after this
)

var (
	errStopped = errors.New("browse is stopping")
	errRetired = errors.New("the device session was closed; try again")
)

// Sessions keeps at most one current remote.Session, shared by every request.
// It dials on first use and logs off after idleTimeout without a lease. One
// mutex guards all of its state; no session is closed and no dial is made
// while holding it.
type Sessions struct {
	ctx  context.Context // the pool's; every dial's context derives from it
	stop context.CancelFunc
	dial func(context.Context) (remote.Session, error)
	log  *slog.Logger

	mu      sync.Mutex
	closed  bool
	cur     *session // the current session, or nil
	pending *dialing // the dial in progress, or nil
}

// session is one dialed session and its pool bookkeeping.
type session struct {
	s      remote.Session
	cancel context.CancelFunc // ends the session's context; called when it retires
	leases int
	idle   *time.Timer // armed while leases is zero
}

// dialing is one dial in progress. sess and err are set before done closes.
type dialing struct {
	done chan struct{}
	sess *session
	err  error
}

// Lease is one request's use of a session. The handler releases it exactly
// once, after every SMB call and the file's Close have returned.
type Lease struct {
	p        *Sessions
	s        *session
	ctx      context.Context
	released bool
}

// NewSessions returns an empty pool. dial opens a session; the context it is
// given is the session's whole lifetime. log gets the pool's lines; the
// caller adds the endpoint to it.
func NewSessions(ctx context.Context, dial func(context.Context) (remote.Session, error), log *slog.Logger) *Sessions {
	ctx, stop := context.WithCancel(ctx)
	return &Sessions{ctx: ctx, stop: stop, dial: dial, log: log}
}

// Get returns a lease on the current session, dialing one if there is none.
// Concurrent calls share one dial. ctx is the request's: Get returns its
// error if it ends first.
func (p *Sessions) Get(ctx context.Context) (*Lease, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errStopped
	}
	if p.cur != nil {
		defer p.mu.Unlock()
		return p.lease(ctx, p.cur), nil
	}
	d := p.pending
	if d == nil {
		d = p.startDial()
	}
	p.mu.Unlock()
	select {
	case <-d.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if d.err != nil {
		return nil, d.err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errStopped
	}
	if p.cur != d.sess {
		return nil, errRetired
	}
	return p.lease(ctx, d.sess), nil
}

// startDial starts a dial; p.mu is held. The dial's context lives as long as
// the session it makes, so its 30 s limit is a timer that cancels it, which
// is stopped once the dial returns, and never a context deadline.
func (p *Sessions) startDial() *dialing {
	d := &dialing{done: make(chan struct{})}
	p.pending = d
	dctx, cancel := context.WithCancel(p.ctx)
	timer := time.AfterFunc(dialTimeout, cancel)
	go func() {
		s, err := p.dial(dctx)
		if !timer.Stop() {
			if err == nil {
				s.Close()
				s = nil
			}
			err = fmt.Errorf("connect to the device: no answer after %v", dialTimeout)
		}
		var sess *session
		p.mu.Lock()
		p.pending = nil
		closed := p.closed
		switch {
		case closed && err == nil:
			err = errStopped
		case err == nil:
			sess = &session{s: s, cancel: cancel}
			p.cur = sess
			p.armIdle(sess)
		}
		d.sess, d.err = sess, err
		close(d.done)
		p.mu.Unlock()
		switch {
		case sess != nil:
			p.log.Info("session opened")
		case closed:
			if s != nil {
				s.Close()
			}
			cancel()
		default:
			cancel()
			p.log.Warn("connect failed", "error", err)
		}
	}()
	return d
}

// lease takes a lease on s; p.mu is held.
func (p *Sessions) lease(ctx context.Context, s *session) *Lease {
	s.leases++
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	l := &Lease{p: p, s: s, ctx: context.WithoutCancel(ctx)}
	return l
}

// armIdle arms s's idle timer; p.mu is held. The callback closes s only if
// this timer is still the armed one, s is still current and unleased, since
// Stop cannot retract a callback that has already started.
func (p *Sessions) armIdle(s *session) {
	var t *time.Timer
	t = time.AfterFunc(idleTimeout, func() {
		p.retire(s, "idle", func() bool { return s.idle == t && s.leases == 0 })
	})
	s.idle = t
}

// retire detaches s if it is still current and ok, called under p.mu,
// agrees, then closes it outside the lock.
func (p *Sessions) retire(s *session, reason string, ok func() bool) {
	p.mu.Lock()
	if p.closed || p.cur != s || !ok() {
		p.mu.Unlock()
		return
	}
	p.cur = nil
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	p.mu.Unlock()
	p.end(s, reason)
}

// end closes a detached session and ends its context.
func (p *Sessions) end(s *session, reason string) {
	s.s.Close()
	s.cancel()
	p.log.Info("session closed", "reason", reason)
}

// Close closes the current session and cancels a dial in progress. Later
// Gets fail without dialing, a dial that still succeeds is closed, and Release
// changes nothing. Close may be called more than once.
func (p *Sessions) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	s := p.cur
	p.cur = nil
	if s != nil && s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	p.mu.Unlock()
	if s != nil {
		p.end(s, "stopped")
	}
	p.stop()
}

// Session is the leased session.
func (l *Lease) Session() remote.Session { return l.s.s }

// Context is for every SMB call made with the lease: it has the request's
// values but not its cancellation, so a call is never abandoned while the
// device may still answer it. The session's own context still ends it.
func (l *Lease) Context() context.Context { return l.ctx }

// Release ends the lease. The last release on the current session arms its
// idle timer; a lease whose session is no longer current changes nothing.
func (l *Lease) Release() {
	p := l.p
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.released {
		return
	}
	l.released = true
	if p.closed || p.cur != l.s {
		return
	}
	l.s.leases--
	if l.s.leases == 0 {
		p.armIdle(l.s)
	}
}
