package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zmerlynn/seestar-copy/internal/plan"
	"github.com/zmerlynn/seestar-copy/internal/remote"
	"github.com/zmerlynn/seestar-copy/internal/state"
)

// share is a fake device over a local folder that a delete run can
// remove from, with a destination and a state folder beside it.
type share struct {
	dir, dest, stateDir string
	faults              map[string]remote.Fault
	last                *remote.FakeDir
}

func newShare(t *testing.T) *share {
	base := t.TempDir()
	s := &share{dir: filepath.Join(base, "share"), dest: filepath.Join(base, "dest"), stateDir: filepath.Join(base, "state")}
	for _, d := range []string{s.dir, s.dest} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// put writes p with data and oldMTime under root.
func put(t *testing.T, root, p, data string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, oldMTime, oldMTime); err != nil {
		t.Fatal(err)
	}
}

// has reports whether p is on the share.
func (s *share) has(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(s.dir, filepath.FromSlash(p)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// record records p as copied with data, as listed with oldMTime.
func (s *share) record(t *testing.T, recs map[string]string) {
	t.Helper()
	st, err := state.Open(s.stateDir, s.dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for p, data := range recs {
		sum := sha256.Sum256([]byte(data))
		rec := state.Record{Path: p, Size: int64(len(data)), MTime: oldMTime, SHA256: hex.EncodeToString(sum[:]), At: oldMTime, How: state.Copied}
		if err := st.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// options returns the options of a real run on the share, proven by the
// copies, of any age.
func (s *share) options() DeleteOptions {
	open := func() (*remote.FakeDir, error) {
		f, err := remote.NewFakeDir(s.dir)
		if err != nil {
			return nil, err
		}
		f.Faults = s.faults
		s.last = f
		return f, nil
	}
	return DeleteOptions{
		Endpoint: "device:445", Dest: s.dest, StateDir: s.stateDir,
		Dial:        func(context.Context, string) (remote.Session, error) { return open() },
		OpenRemover: func(context.Context, string) (remote.Remover, error) { return open() },
	}
}

// outcomes renders a result's files as "path outcome", for comparison.
func outcomes(r DeleteResult) []string {
	names := []string{FileDeleted: "deleted", FileKept: "kept", FileRecent: "recent", FileFailed: "failed"}
	var out []string
	for _, f := range r.Files {
		out = append(out, f.Path+" "+names[f.Outcome])
	}
	return out
}

// A preview and a real run return each file's outcome and call the hooks;
// only the real run calls Deleted.
func TestDeleteResultAndHooks(t *testing.T) {
	s := newShare(t)
	files := map[string]string{"MyWorks/a.jpg": "aaa", "MyWorks/b.jpg": "bb"}
	for p, data := range files {
		put(t, s.dir, p, data)
		put(t, s.dest, p, data)
	}
	put(t, s.dir, "MyWorks/new.jpg", "n") // not in the state
	s.record(t, files)
	for _, dry := range []bool{true, false} {
		o := s.options()
		o.DryRun = dry
		var listed, checked []int
		var hashed [][2]int64
		var deleted []string
		o.Hooks = Hooks{
			Listed:  func(n int) { listed = append(listed, n) },
			Checked: func(n int) { checked = append(checked, n) },
			Hashed:  func(done, total int64) { hashed = append(hashed, [2]int64{done, total}) },
			Deleted: func(p string, size int64) { deleted = append(deleted, fmt.Sprint(p, " ", size)) },
		}
		r, err := RunDelete(t.Context(), o, discard)
		if err != nil {
			t.Fatalf("dry run %v: %v", dry, err)
		}
		// The run goes through the listing in reverse.
		want := []string{"MyWorks/new.jpg kept", "MyWorks/b.jpg deleted", "MyWorks/a.jpg deleted"}
		if got := outcomes(r); !slices.Equal(got, want) {
			t.Errorf("dry run %v: got outcomes %q, want %q", dry, got, want)
		}
		if f := r.Files[0]; f.Reason != plan.KeepNotState || f.Size != 1 {
			t.Errorf("dry run %v: got %+v, want size 1 and reason %q", dry, f, plan.KeepNotState)
		}
		if r.Folders != 0 {
			t.Errorf("dry run %v: got %d folders, want 0: MyWorks keeps new.jpg", dry, r.Folders)
		}
		if !slices.Equal(listed, []int{1}) || !slices.Equal(checked, []int{3}) {
			t.Errorf("dry run %v: got Listed %v and Checked %v, want [1] and [3]", dry, listed, checked)
		}
		if want := [][2]int64{{2, 5}, {5, 5}}; !slices.Equal(hashed, want) {
			t.Errorf("dry run %v: got Hashed %v, want %v", dry, hashed, want)
		}
		var wantDeleted []string
		if !dry {
			wantDeleted = []string{"MyWorks/b.jpg 2", "MyWorks/a.jpg 3"}
		}
		if !slices.Equal(deleted, wantDeleted) {
			t.Errorf("dry run %v: got Deleted %q, want %q", dry, deleted, wantDeleted)
		}
	}
	if s.has(t, "MyWorks/a.jpg") || s.has(t, "MyWorks/b.jpg") || !s.has(t, "MyWorks/new.jpg") {
		t.Error("after the real run: want a.jpg and b.jpg gone and new.jpg kept")
	}
}

// A failed removal is in the result with its reason.
func TestDeleteResultFailed(t *testing.T) {
	s := newShare(t)
	put(t, s.dir, "MyWorks/a.jpg", "a")
	o := s.options()
	o.IgnoreState = true
	s.faults = map[string]remote.Fault{"MyWorks/a.jpg": {BeforeRecheck: func() { s.last.Close() }}}
	r, err := RunDelete(t.Context(), o, discard)
	if err == nil {
		t.Fatal("got no error, want the failed removal")
	}
	if got, want := outcomes(r), []string{"MyWorks/a.jpg failed"}; !slices.Equal(got, want) {
		t.Fatalf("got outcomes %q, want %q", got, want)
	}
	if r.Files[0].Reason != err.Error() {
		t.Errorf("got reason %q, want the run's error %q", r.Files[0].Reason, err)
	}
}

// Only limits a real run to the files it holds: nil is every file, empty
// is none, and a file outside it, such as one that became eligible after
// the preview or one in an unchecked folder, is left alone. A file in it
// is still checked.
func TestDeleteOnly(t *testing.T) {
	files := map[string]string{"MyWorks/a.jpg": "a", "MyWorks/late.jpg": "l", "Other/c.jpg": "c", "MyWorks/changed.jpg": "x"}
	for _, tc := range []struct {
		name string
		only map[string]bool
		want []string // the outcomes
		gone []string
	}{
		{"nil", nil, []string{"Other/c.jpg deleted", "MyWorks/late.jpg deleted", "MyWorks/changed.jpg kept", "MyWorks/a.jpg deleted"},
			[]string{"MyWorks/a.jpg", "MyWorks/late.jpg", "Other", "Other/c.jpg"}},
		{"empty", map[string]bool{}, nil, nil},
		{"preview", map[string]bool{"MyWorks/a.jpg": true, "MyWorks/changed.jpg": true},
			[]string{"MyWorks/changed.jpg kept", "MyWorks/a.jpg deleted"}, []string{"MyWorks/a.jpg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newShare(t)
			for p, data := range files {
				put(t, s.dir, p, data)
			}
			// The state was right for changed.jpg when it was previewed;
			// the device has changed it since.
			s.record(t, map[string]string{"MyWorks/a.jpg": "a", "MyWorks/late.jpg": "l", "Other/c.jpg": "c", "MyWorks/changed.jpg": "xy"})
			o := s.options()
			o.TrustState, o.Only = true, tc.only
			r, err := RunDelete(t.Context(), o, discard)
			if err != nil {
				t.Fatal(err)
			}
			if got := outcomes(r); !slices.Equal(got, tc.want) {
				t.Errorf("got outcomes %q, want %q", got, tc.want)
			}
			for _, p := range []string{"MyWorks/a.jpg", "MyWorks/late.jpg", "MyWorks/changed.jpg", "Other", "Other/c.jpg"} {
				if gone := !s.has(t, p); gone != slices.Contains(tc.gone, p) {
					t.Errorf("%s: gone %v, want %v", p, gone, !gone)
				}
			}
		})
	}
}

// Stop during a removal lets that file finish and count, and starts no
// other file or folder.
func TestDeleteStop(t *testing.T) {
	s := newShare(t)
	for _, p := range []string{"MyWorks/a.jpg", "MyWorks/b.jpg", "MyWorks/c.jpg"} {
		put(t, s.dir, p, "x")
	}
	stop := make(chan struct{})
	// The run goes in reverse: c.jpg, then b.jpg, which Stop holds up.
	s.faults = map[string]remote.Fault{"MyWorks/b.jpg": {BeforeRecheck: func() { close(stop) }}}
	o := s.options()
	o.IgnoreState, o.Stop = true, stop
	var deleted []string
	o.Hooks.Deleted = func(p string, _ int64) { deleted = append(deleted, p) }
	var logs bytes.Buffer
	r, err := RunDelete(t.Context(), o, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"MyWorks/c.jpg", "MyWorks/b.jpg"}
	if !slices.Equal(deleted, want) {
		t.Errorf("Deleted: got %q, want %q", deleted, want)
	}
	if got, want := outcomes(r), []string{"MyWorks/c.jpg deleted", "MyWorks/b.jpg deleted"}; !slices.Equal(got, want) {
		t.Errorf("got outcomes %q, want %q", got, want)
	}
	if calls, _ := s.last.Counts(); calls != 2 || !s.has(t, "MyWorks/a.jpg") || !s.has(t, "MyWorks") {
		t.Errorf("got %d remove calls, want 2, with a.jpg and MyWorks kept", calls)
	}
	if !strings.Contains(logs.String(), `msg="deletion stopped" deleted=2 `) {
		t.Errorf("want a deletion stopped line with deleted=2 in:\n%s", logs.String())
	}
}
