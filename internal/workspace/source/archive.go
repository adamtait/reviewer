// SPDX-License-Identifier: MIT

// Package source holds a repository's files at a commit, in memory.
//
// The service stores nothing (ADR-0029). When a user opens a commit, its files
// are read from GitHub as one archive, with that user's token, and kept in
// memory for as long as the cache has room. The file list, a file's text and
// text search are all answered from there (ADR-0030).
package source

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Kind says what is held for a path.
type Kind string

const (
	// KindText is a file whose text is held.
	KindText Kind = "text"
	// KindBinary is a file that is not valid UTF-8, or holds a NUL byte.
	KindBinary Kind = "binary"
	// KindTooLarge is a file over the per-file limit.
	KindTooLarge Kind = "too_large"
	// KindOther is anything that is not a regular file: a link, or a submodule.
	KindOther Kind = "other"
)

// Entry is one path in a commit.
type Entry struct {
	Path string
	Kind Kind
}

// Commit is a commit's files. It is never changed once built, so any number of
// requests may read it at once.
type Commit struct {
	entries []Entry
	kinds   map[string]Kind
	text    map[string]string
	size    int64
}

// Entries returns every path in the commit, sorted.
func (c *Commit) Entries() []Entry { return c.entries }

// Kind returns what is held for a path, and whether the path is in the commit.
func (c *Commit) Kind(p string) (Kind, bool) {
	k, ok := c.kinds[p]
	return k, ok
}

// Text returns a file's text. ok is false for a path that is absent or whose
// text is not held.
func (c *Commit) Text(p string) (text string, ok bool) {
	text, ok = c.text[p]
	return text, ok
}

// Size is what the commit costs to hold, in bytes: its text and its paths.
func (c *Commit) Size() int64 { return c.size }

// Limits bound what unpacking will hold.
type Limits struct {
	// ArchiveBytes is the most kept from one commit: its text and its paths.
	ArchiveBytes int64
	// FileBytes is the most text kept from one file.
	FileBytes int64
}

// ErrTooLarge means the commit's text and paths pass Limits.ArchiveBytes.
var ErrTooLarge = errors.New("the commit holds more text than the configured limit")

// maxEntries bounds the number of paths. GitHub would not send an archive like
// this, but the bound costs nothing and a tar stream is otherwise unbounded.
const maxEntries = 500_000

// Unpack reads a gzip-compressed tar stream, as GitHub's archive call sends it,
// into memory.
//
// Every path in such an archive sits under one top-level folder, which is
// dropped. A file is kept as text if it is valid UTF-8, holds no NUL byte and
// is within the per-file limit; otherwise only its path is kept, with a Kind
// that says why. Nothing is written to disk and nothing in the archive is
// executed or followed: a link is recorded as a path and not resolved.
func Unpack(r io.Reader, limits Limits) (*Commit, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("the archive is not gzip: %w", err)
	}
	defer func() { _ = zr.Close() }()

	c := &Commit{kinds: map[string]Kind{}, text: map[string]string{}}
	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeDir {
			continue
		}
		name, ok := insideTopFolder(header.Name)
		if !ok {
			continue
		}
		if len(c.kinds) >= maxEntries {
			return nil, fmt.Errorf("the archive has more than %d entries", maxEntries)
		}

		kind := KindOther
		if header.Typeflag == tar.TypeReg {
			switch {
			case header.Size > limits.FileBytes:
				kind = KindTooLarge
			default:
				// LimitReader as well as the size check: the header's size is a
				// claim, and the limit must hold even if the claim is false.
				raw, err := io.ReadAll(io.LimitReader(tr, limits.FileBytes+1))
				if err != nil {
					return nil, fmt.Errorf("reading %s from the archive: %w", name, err)
				}
				switch {
				case int64(len(raw)) > limits.FileBytes:
					kind = KindTooLarge
				case !isText(raw):
					kind = KindBinary
				default:
					kind = KindText
					c.size += int64(len(raw)) - int64(len(c.text[name]))
					c.text[name] = string(raw)
				}
			}
		}
		if _, seen := c.kinds[name]; !seen {
			c.size += int64(len(name))
		}
		if kind != KindText {
			// A path that appears twice keeps its last entry, as tar does.
			c.size -= int64(len(c.text[name]))
			delete(c.text, name)
		}
		c.kinds[name] = kind
		if c.size > limits.ArchiveBytes {
			return nil, ErrTooLarge
		}
	}

	c.entries = make([]Entry, 0, len(c.kinds))
	for p, k := range c.kinds {
		c.entries = append(c.entries, Entry{Path: p, Kind: k})
	}
	sort.Slice(c.entries, func(i, j int) bool { return c.entries[i].Path < c.entries[j].Path })
	return c, nil
}

// insideTopFolder drops an archive path's first component. It refuses a path
// with a ".." in it rather than working out where it leads: nothing here is
// written to disk, so such a path could do no harm, but a path that means
// something other than what it says has no place in a list shown to a user.
func insideTopFolder(name string) (string, bool) {
	var parts []string
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "", ".":
			// A leading slash, a doubled slash, a trailing slash, "./".
		case "..":
			return "", false
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) < 2 {
		return "", false
	}
	return path.Join(parts[1:]...), true
}

// isText reports whether content is text the service will hold and show.
func isText(raw []byte) bool {
	for _, b := range raw {
		if b == 0 {
			return false
		}
	}
	return utf8.Valid(raw)
}
