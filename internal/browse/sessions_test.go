package browse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"
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

func TestIdleClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}}
		p, logs := newPool(d)
		defer p.Close()
		l := mustGet(t, p, context.Background())
		l.Release()
		time.Sleep(idleTimeout - time.Nanosecond)
		synctest.Wait()
		if d.session(0).closed() {
			t.Fatalf("closed before %v", idleTimeout)
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if !d.session(0).closed() {
			t.Fatalf("open %v after the last release", idleTimeout)
		}
		if logs.count("session closed", "reason=idle") != 1 {
			t.Errorf("logs:\n%s", logs)
		}
		l = mustGet(t, p, context.Background())
		defer l.Release()
		if d.dials() != 2 || l.Session() != d.session(1) {
			t.Errorf("Get after an idle close did not dial again")
		}
	})
}

func TestHeldLeaseKeepsSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}}
		p, _ := newPool(d)
		defer p.Close()
		l := mustGet(t, p, context.Background())
		time.Sleep(10 * idleTimeout)
		synctest.Wait()
		if d.session(0).closed() {
			t.Fatal("a held lease did not keep the session")
		}
		l.Release()
	})
}

func TestStaleIdleTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}}
		p, _ := newPool(d)
		defer p.Close()
		mustGet(t, p, context.Background()).Release() // 0 s
		time.Sleep(10 * time.Second)
		l := mustGet(t, p, context.Background()) // 10 s
		time.Sleep(10 * time.Second)
		l.Release() // 20 s
		time.Sleep(idleTimeout - time.Nanosecond)
		synctest.Wait()
		if d.session(0).closed() {
			t.Fatal("closed by the timer of the first release")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if !d.session(0).closed() {
			t.Fatal("not closed 30 s after the last release")
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

func TestFailedDialCancelsItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dialer{fsys: fstest.MapFS{}, err: errors.New("no route")}
		p, logs := newPool(d)
		defer p.Close()
		if _, err := p.Get(context.Background()); err == nil || err.Error() != "no route" {
			t.Fatalf("Get = %v, want the dial error", err)
		}
		synctest.Wait()
		if d.session(0).ctx.Err() == nil {
			t.Error("a failed dial's context is still live")
		}
		if logs.count("connect failed", "no route") != 1 {
			t.Errorf("logs:\n%s", logs)
		}
		d.err = nil
		l := mustGet(t, p, context.Background())
		l.Release()
		if d.dials() != 2 {
			t.Errorf("a failed dial was remembered: dials = %d, want 2", d.dials())
		}
	})
}
