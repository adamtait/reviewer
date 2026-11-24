// SPDX-License-Identifier: MIT

package source

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"time"
)

// Key names one thing held in memory.
//
// Owner and Repo are part of the key, and not only the commit hash, on purpose.
// A hash alone would let a user who can read one connected repository ask for a
// commit of another by its hash and be served it from memory, without GitHub
// ever being asked whether they may read it. With the repository in the key, an
// entry is only ever found by a request for that repository, and every such
// request has already passed the access check for it.
type Key struct {
	Owner, Repo, SHA string
	// What separates the kinds of thing held for one commit: its files, its
	// navigation index.
	What string
}

// NewKey returns a Key with the owner and name folded to lower case, the way
// GitHub matches them, so two spellings of one repository share an entry.
func NewKey(owner, repo, sha, what string) Key {
	return Key{Owner: strings.ToLower(owner), Repo: strings.ToLower(repo), SHA: strings.ToLower(sha), What: what}
}

// Sized is anything the cache can hold: it knows what it costs.
type Sized interface {
	// Size is the cost in bytes. It is an estimate and a low one: it counts
	// the content, not the maps and headers that hold it.
	Size() int64
}

// maxLoads is how many loads may run at once, across all keys. Each one builds
// its result beside whatever the cache already holds, so without a bound a
// burst of first opens could ask for several archives' worth of memory at once.
// Two, because opening a pull request reads two commits, its head and its merge
// base; a third load waits its turn. The operating guide's memory sizing rule
// counts on this number.
const maxLoads = 2

// failureCost is what a remembered failure is charged against the cache.
const failureCost = 256

// Cache holds loaded values up to a total size, dropping the least recently
// used to make room. A value is loaded at most once at a time for each key:
// requests that arrive while a load is running wait for it.
type Cache struct {
	max     int64
	timeout time.Duration
	slots   chan struct{}

	mu      sync.Mutex
	used    int64
	order   *list.List // front is the most recently used
	items   map[Key]*list.Element
	flights map[Key]*flight
}

type item struct {
	key   Key
	value Sized
	// err is set for a remembered failure. See Permanent.
	err  error
	cost int64
}

type flight struct {
	done  chan struct{}
	value Sized
	err   error
}

// NewCache returns a cache that holds up to maxBytes and gives each load up to
// timeout, counted from the first request for it.
func NewCache(maxBytes int64, timeout time.Duration) *Cache {
	return &Cache{
		max:     maxBytes,
		timeout: timeout,
		slots:   make(chan struct{}, maxLoads),
		order:   list.New(),
		items:   map[Key]*list.Element{},
		flights: map[Key]*flight{},
	}
}

// Permanent marks a load's failure as one that would repeat: asking again would
// cost the same fetch and reach the same answer. The cache remembers it and
// answers later requests for the key with it, until the entry is dropped to
// make room. A commit never changes, so "too large" is true of it for good.
type Permanent struct{ Err error }

func (p Permanent) Error() string { return p.Err.Error() }
func (p Permanent) Unwrap() error { return p.Err }

// Get returns the value for key, calling load if it is not held.
//
// load runs under its own deadline and not under ctx, so that when the first
// of several waiting requests is abandoned the others still get their answer.
// A caller whose own ctx ends stops waiting and gets ctx's error; the load
// carries on and its result is kept for the next request. A failed load is not
// remembered unless its error is a Permanent.
func (c *Cache) Get(ctx context.Context, key Key, load func(context.Context) (Sized, error)) (Sized, error) {
	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		it := el.Value.(*item)
		c.mu.Unlock()
		return it.value, it.err
	}
	f, running := c.flights[key]
	if !running {
		f = &flight{done: make(chan struct{})}
		c.flights[key] = f
		go c.run(key, f, load)
	}
	c.mu.Unlock()

	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Cache) run(key Key, f *flight, load func(context.Context) (Sized, error)) {
	// Detached from any request on purpose; see Get. The deadline is what stops it.
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	select {
	case c.slots <- struct{}{}:
		f.value, f.err = protect(ctx, load)
		<-c.slots
	case <-ctx.Done():
		f.err = ctx.Err()
	}

	c.mu.Lock()
	delete(c.flights, key)
	switch {
	case f.err == nil:
		c.add(&item{key: key, value: f.value, cost: f.value.Size()})
	case isPermanent(f.err):
		c.add(&item{key: key, err: f.err, cost: failureCost})
	}
	c.mu.Unlock()
	close(f.done)
}

// protect turns a panic in a load into an error. The load runs on its own
// goroutine, where a panic would otherwise end the process, and the archive it
// reads comes from outside.
func protect(ctx context.Context, load func(context.Context) (Sized, error)) (value Sized, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value, err = nil, errLoadPanicked
		}
	}()
	value, err = load(ctx)
	if err == nil && value == nil {
		return nil, errLoadPanicked
	}
	return value, err
}

// add stores an item, dropping the least recently used entries until it fits.
// An item larger than the whole cache is not stored. The caller holds the lock.
func (c *Cache) add(it *item) {
	if it.cost > c.max {
		return
	}
	for c.used+it.cost > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		dropped := c.order.Remove(oldest).(*item)
		delete(c.items, dropped.key)
		c.used -= dropped.cost
	}
	c.items[it.key] = c.order.PushFront(it)
	c.used += it.cost
}

// Stats is what the cache holds now.
type Stats struct {
	Entries int
	Bytes   int64
}

// Stats reports what the cache holds.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Entries: len(c.items), Bytes: c.used}
}
