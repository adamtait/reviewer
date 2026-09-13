// SPDX-License-Identifier: MIT

package pluginhost

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/adamtait/reviewer/pkg/plugin"
)

// AddLocal registers a plugin that is compiled into this binary.
//
// ADR-0006 makes the protocol the only path to an analyzer, and ADR-0027 keeps
// that true for built-in analyzers without paying for a subprocess: the handler
// runs in a goroutine and the two sides are joined by in-memory pipes, so the
// frames, the descriptor validation and the lane gate are all exactly the same
// code as for an external plugin. Only the transport differs.
func (m *Manager) AddLocal(ctx context.Context, h plugin.Handler, timeout time.Duration) error {
	// hostToPlugin carries frames one way, pluginToHost the other.
	hostRead, hostWrite := io.Pipe()
	pluginRead, pluginWrite := io.Pipe()

	c := &conn{
		id:      h.Name(),
		stdin:   hostWrite,
		in:      plugin.NewReader(pluginRead),
		out:     plugin.NewWriter(hostWrite),
		timeout: timeout,
		exited:  make(chan struct{}),
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer close(c.exited)
		// Serve owns the plugin side; closing its writer is what gives the host
		// an EOF rather than a hang when the handler returns.
		defer func() {
			_ = pluginWrite.Close()
			_ = hostRead.Close()
		}()
		if err := serveLocal(h, hostRead, pluginWrite, m.log); err != nil {
			fmt.Fprintf(m.log, "%s: %v\n", h.Name(), err)
		}
	}()

	if err := c.handshake(ctx, m.hostID, handshakeDeadline(timeout)); err != nil {
		c.shutdownLocal()
		return fmt.Errorf("built-in plugin %s: %w", h.Name(), err)
	}

	m.mu.Lock()
	m.conns = append(m.conns, c)
	m.mu.Unlock()
	return nil
}

// serveLocal is plugin.Serve against explicit streams rather than stdio.
func serveLocal(h plugin.Handler, r io.Reader, w io.Writer, logw io.Writer) error {
	return plugin.ServeStreams(h, r, w, logw)
}

// shutdownLocal closes the host's writer, which the handler sees as end of
// stream. A built-in plugin has no process group to kill.
func (c *conn) shutdownLocal() {
	_ = c.stdin.Close()
	<-c.exited
	c.wg.Wait()
}

// isLocal reports whether this conn is an in-process handler rather than a child
// process. Shutdown and failure handling differ: there is nothing to signal.
func (c *conn) isLocal() bool { return c.cmd == nil }
