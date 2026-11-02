package main

import (
	"context"
	"path"
	"strings"

	"github.com/spf13/pflag"
	"github.com/zmerlynn/seestar-copy/internal/remote"
)

// defaultExcludes are the built-in exclude patterns. Each one is checked and
// matched exactly as the same --exclude flag is, and the help shows them as
// those flags.
var defaultExcludes = []string{
	".*",                        // dot names: .DS_Store, .StackReplayTemp, ._*
	"*.tmp",                     // unfinished files, such as a video being written
	"Thumbs.db",                 // Windows thumbnail caches
	"desktop.ini",               // Windows folder settings
	"$RECYCLE.BIN",              // the Windows recycle bin
	"System Volume Information", // Windows volume data
	"/log",                      // the telescope's logs
	"/tmp_stitch",               // the telescope's scratch folders
	"/batch_stack_tmp",
}

// selectionFlags are the flags that choose which part of the share a
// command works on. Every such command registers them with
// [selectionFlags.register] and checks them with [selectionFlags.parse], so
// they mean the same everywhere.
type selectionFlags struct {
	excludes          []string
	noDefaultExcludes bool
}

func (f *selectionFlags) register(fs *pflag.FlagSet) {
	fs.StringArrayVar(&f.excludes, "exclude", nil, "skip files and folders that match this pattern, as well as the built-in ones; repeatable")
	fs.BoolVar(&f.noDefaultExcludes, "no-default-excludes", false, "do not skip what the built-in patterns match")
}

// parse checks the flags, without touching the network or the disk.
func (f selectionFlags) parse() (selector, error) {
	all := f.excludes
	if !f.noDefaultExcludes {
		all = append(append([]string{}, defaultExcludes...), f.excludes...)
	}
	var s selector
	for _, flag := range all {
		p, err := parsePattern("--exclude", flag)
		if err != nil {
			return selector{}, err
		}
		s.excludes = append(s.excludes, p)
	}
	return s, nil
}

// selector is a checked selection: what a walk of the share leaves out.
type selector struct {
	excludes []pattern
}

// skippedEntry is a listed entry that an exclude matched.
type skippedEntry struct {
	path string
	// by is the first exclude that matched.
	by pattern
}

// walk lists the share over sess, leaving out what the selection does, and
// returns what it kept and the entries it skipped, in listing order. A
// skipped folder's contents are not listed. Any listing failure, of the
// root or of a folder that is not skipped, fails the walk; the entries
// skipped before it are still returned.
func (s selector) walk(ctx context.Context, sess remote.Session) ([]remote.Entry, []skippedEntry, error) {
	var skipped []skippedEntry
	entries, err := sess.Walk(ctx, func(p string, _ bool) bool {
		for _, e := range s.excludes {
			if e.match(p) {
				skipped = append(skipped, skippedEntry{p, e})
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, skipped, err
	}
	return entries, skipped, nil
}

// pattern is one checked selection pattern.
type pattern struct {
	// flag is the pattern as given, for logs.
	flag string
	// pattern is lowercased and has no leading slash. If anchored, it is
	// matched against the whole share path; otherwise against the last name.
	pattern  string
	anchored bool
}

// parsePattern checks the value of option, a pattern flag. A pattern with
// no slash matches a name at any depth; one with a slash anywhere is a path
// from the share root, with one leading slash allowed. Only literal
// characters, * and ? are allowed, so path.Match never sees a class, an
// escape or a bad pattern. Nothing is cleaned: a pattern that would need it
// is refused.
func parsePattern(option, flag string) (pattern, error) {
	bad := func(rule string) (pattern, error) { return pattern{}, &usageError{Option: option, Rule: rule} }
	switch {
	case strings.ContainsAny(flag, `[\`):
		return bad(`[ and \ are not supported; use * and ?, and / between folders`)
	case strings.Contains(flag, "**"):
		return bad("** is not supported; * matches within one name")
	case len(flag) > 1 && strings.HasSuffix(flag, "/"):
		return bad("omit the trailing /; a pattern matches files and folders")
	}
	p := strings.TrimPrefix(flag, "/")
	for elem := range strings.SplitSeq(p, "/") {
		if elem == "" || elem == "." || elem == ".." {
			return bad("use a name, or a path from the share root, with no empty, . or .. parts")
		}
	}
	return pattern{flag: flag, pattern: strings.ToLower(p), anchored: strings.Contains(flag, "/")}, nil
}

// match reports whether the share path p matches, ignoring case by
// lowercasing both sides; there is no Unicode normalization.
func (m pattern) match(p string) bool {
	if !m.anchored {
		p = path.Base(p)
	}
	ok, _ := path.Match(m.pattern, strings.ToLower(p))
	return ok
}
