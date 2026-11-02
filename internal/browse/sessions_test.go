package browse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
)

func newPool(d *dialer) (*Sessions, *logBuffer) {
	log, buf := newLog()
	return NewSessions(context.Background(), d.dial, log), buf
}

func TestGetSharesOneDial(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}, gate: make(chan struct{})}
		p, _ := newPool(d)
		defer p.Close()
		var wg sync.WaitGroup
		leases := make(chan *Lease, 10)
		for range 10 {
			wg.Go(func() {
				l, err := p.Get(context.Background())
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				leases <- l
			})
		}
		ctx, cancel := context.WithCancel(context.Background())
		gone := make(chan error)
		go func() {
			_, err := p.Get(ctx)
			gone <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-gone; !errors.Is(err, context.Canceled) {
			t.Errorf("canceled waiter: Get = %v, want context.Canceled", err)
		}
		close(d.gate)
		wg.Wait()
		close(leases)
		for l := range leases {
			if l.Session() != d.session(0) {
				t.Errorf("a lease is on another session")
			}
			l.Release()
		}
		if n := d.dials(); n != 1 {
			t.Errorf("dials = %d, want 1", n)
		}
	})
}

func TestCloseIsIdempotent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}}
		p, logs := newPool(d)
		l := mustGet(t, p, context.Background())
		p.Close()
		p.Close()
		s := d.session(0)
		if s.n("Close") != 1 || s.ctx.Err() == nil {
			t.Errorf("Close calls = %d, context %v; want 1 and canceled", s.n("Close"), s.ctx.Err())
		}
		l.Release()
		if logs.count("session closed", "reason=stopped") != 1 {
			t.Errorf("logs:\n%s", logs)
		}
	})
}
