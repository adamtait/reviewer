// SPDX-License-Identifier: MIT

package plugin

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Handler is what a Go plugin implements.
//
// Describe is called once, after the handshake. Analyze is called once per
// analyzer the host chooses to run, never concurrently, and only for IDs that
// Describe reported. A handler may hold expensive state between Analyze calls —
// that is the reason the process is long-lived.
type Handler interface {
	// Name and Version identify the plugin in the run log and in version-skew
	// warnings.
	Name() string
	Version() string

	// Describe lists the analyzers this plugin provides in this repository. It is
	// the right place to decide Available: probing for an ESLint config here costs
	// one filesystem call and saves the host a pointless Analyze round trip.
	Describe() []Descriptor

	// Analyze runs one analyzer. Returning an error is not fatal to the run: the
	// host records it and continues without this analyzer's findings. Warnings are
	// for things worth saying that are not failures.
	Analyze(id string, req AnalyzeRequest) (findings []finding.Finding, warnings []string, err error)
}

// Serve runs the protocol conversation against r and w until the host says bye or
// the stream closes. It is the whole SDK: a Go plugin is a Handler plus
//
//	func main() { plugin.Serve(myHandler{}) }
//
// Serve writes protocol frames to w and nothing else, so a plugin must keep its
// own diagnostics off that stream.
func Serve(h Handler) error {
	return serve(h, os.Stdin, os.Stdout, os.Stderr)
}

func serve(h Handler, r io.Reader, w, logw io.Writer) error {
	in, out := NewReader(r), NewWriter(w)
	greeted := false

	for {
		f, err := in.Read()
		switch {
		case errors.Is(err, ErrClosed):
			// The host went away without saying bye. Not an error: it may have
			// timed out or been killed, and there is nothing to clean up.
			return nil
		case err != nil:
			// A line we cannot parse. Say so and keep reading: the host may
			// recover, and exiting would lose the analyzers we could still run.
			fmt.Fprintf(logw, "%s: %v\n", h.Name(), err)
			if writeErr := out.Write(Frame{Type: TypeError, Message: err.Error()}); writeErr != nil {
				return writeErr
			}
			continue
		}

		switch f.Type {
		case TypeHello:
			if f.Protocol != Protocol {
				// Refuse explicitly rather than guessing at an unknown dialect.
				err := fmt.Errorf("host speaks protocol %d, this plugin speaks %d", f.Protocol, Protocol)
				_ = out.Write(Frame{Type: TypeError, Message: err.Error()})
				return err
			}
			greeted = true
			if err := out.Write(Frame{
				Type:     TypeHello,
				Protocol: Protocol,
				Plugin:   h.Name(),
				Version:  h.Version(),
			}); err != nil {
				return err
			}

		case TypeDescribe:
			if !greeted {
				return errUngreeted(out, f.Type)
			}
			if err := out.Write(Frame{Type: TypeDescribe, Analyzers: h.Describe()}); err != nil {
				return err
			}

		case TypeAnalyze:
			if !greeted {
				return errUngreeted(out, f.Type)
			}
			req := AnalyzeRequest{}
			if f.Request != nil {
				req = *f.Request
			}
			findings, warnings, err := h.Analyze(f.Analyzer, req)
			if err != nil {
				fmt.Fprintf(logw, "%s: %s: %v\n", h.Name(), f.Analyzer, err)
				if err := out.Write(Frame{
					Type:     TypeError,
					Analyzer: f.Analyzer,
					Message:  err.Error(),
				}); err != nil {
					return err
				}
				continue
			}
			if err := out.Write(Frame{
				Type:     TypeFindings,
				Analyzer: f.Analyzer,
				Findings: findings,
				Warnings: warnings,
			}); err != nil {
				return err
			}

		case TypeBye:
			return nil

		default:
			// An unknown frame type from a newer host. Ignoring it is what keeps
			// the protocol additively extensible.
			fmt.Fprintf(logw, "%s: ignoring unknown frame type %q\n", h.Name(), f.Type)
		}
	}
}

func errUngreeted(out *Writer, got FrameType) error {
	err := fmt.Errorf("received %s before hello", got)
	_ = out.Write(Frame{Type: TypeError, Message: err.Error()})
	return err
}
