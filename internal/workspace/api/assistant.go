// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/assistant"
	"github.com/adamtait/reviewer/internal/workspace/catalog"
	"github.com/adamtait/reviewer/internal/workspace/gateway"
	"github.com/adamtait/reviewer/internal/workspace/obs"
)

// maxAskBody bounds a question's request: the question, the selection and the
// conversation so far.
const maxAskBody = 512 << 10

// model returns the model configured for a repository, and whether it has an
// assistant.
func (s *Server) model(owner, repo string) (string, bool) {
	if s.Gateway == nil {
		return "", false
	}
	return s.Gateway.Model(owner, repo)
}

// question answers one question about a commit, as a stream of events.
//
// One question is one request. The browser holds the conversation and sends
// it along; the service keeps nothing between questions. The answer is
// written as it arrives from the model, and if the browser goes away the
// request's context ends and the question with it.
func (s *Server) question(w http.ResponseWriter, r *http.Request) {
	fields := obs.From(r.Context())
	var body apitypes.Ask
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAskBody))
	if err != nil || json.Unmarshal(raw, &body) != nil {
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "the request body must be a question")
		return
	}
	commit, ok := s.commit(w, r)
	if !ok {
		return
	}
	repo, client, ok := s.asUser(w, r)
	if !ok {
		return
	}
	if _, has := s.model(repo.Owner, repo.Name); !has {
		WriteError(w, http.StatusNotFound, apitypes.CodeNoAssistant, "This repository has no assistant configured.")
		return
	}
	session, _ := SessionFrom(r.Context())
	sha := r.PathValue("sha")

	ask := assistant.Ask{Text: body.Text}
	for _, m := range body.History {
		ask.History = append(ask.History, assistant.Message{Role: m.Role, Text: m.Text})
	}
	if sel := body.Selection; sel != nil {
		// A selection is of the commit the question is about. One from another
		// commit would be quoted from a file the tools then read differently.
		if sel.SHA != sha {
			WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, "the selection is from another commit than the question")
			return
		}
		ask.Selection = &assistant.Selection{Path: sel.Path, StartLine: sel.StartLine, EndLine: sel.EndLine}
	}

	subject := assistant.Subject{Owner: repo.Owner, Repo: repo.Name, SHA: sha, UserLogin: session.Login, Commit: commit}
	// The index, if the commit has one. A question does not fail for want of
	// it: the lookup tools fall back to text search and say so.
	if index, _, err := s.Navigation.Lookup(r.Context(), client, repo.Owner, repo.Name, sha); err == nil {
		subject.Index = index
	}
	if body.PullNumber > 0 {
		fields.SetPull(body.PullNumber)
		// Likewise the pull request: without it, the two tools that read it
		// tell the model there is none.
		if view, err := catalog.View(r.Context(), client, repo, body.PullNumber, s.WebBaseURL); err == nil {
			pull := &assistant.Pull{Number: view.Number, Title: view.Title, Description: view.Body}
			for _, f := range view.Files {
				pull.Files = append(pull.Files, assistant.ChangedFile{Path: f.Path, PreviousPath: f.PreviousPath, Status: f.Status})
			}
			subject.Pull = pull
		}
	}

	call, err := assistant.Call(subject, ask)
	if err != nil {
		message := "the question cannot be asked"
		if errors.Is(err, assistant.ErrBadAsk) {
			// The reason, without the sentinel's own words in front of it.
			message = "The question cannot be asked: " + strings.TrimPrefix(err.Error(), assistant.ErrBadAsk.Error()+": ")
		}
		WriteError(w, http.StatusBadRequest, apitypes.CodeBadRequest, message)
		return
	}

	// From here on the response is a stream, and whatever happens is an event.
	stream := newEventStream(w)
	result := s.Gateway.Answer(r.Context(), call, assistant.Tools(subject), func(ev gateway.Event) {
		switch e := ev.(type) {
		case gateway.Delta:
			stream.send(apitypes.EventDelta, apitypes.Delta{Text: e.Text})
		case gateway.Excerpt:
			// The gateway counts lines from 1, inclusive, as the model reads
			// them. The API counts from 0 with an exclusive end.
			stream.send(apitypes.EventExcerpt, apitypes.Excerpt{SHA: sha, Path: e.Path, StartLine: e.StartLine - 1, EndLine: e.EndLine})
		case gateway.Withheld:
			stream.send(apitypes.EventWithheld, apitypes.Withheld{What: e.What, Reason: e.Reason})
		case gateway.Waiting:
			stream.send(apitypes.EventWaiting, apitypes.Waiting{Seconds: e.Seconds, Reason: e.Reason})
		}
	})
	fields.SetOutcome(result.Outcome)
	if result.Outcome == gateway.OutcomeOK {
		stream.send(apitypes.EventDone, apitypes.Done{
			Model: result.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
			ReasoningTokens: result.ReasoningTokens, ModelCalls: result.ModelCalls,
		})
		return
	}
	stream.send(apitypes.EventStopped, apitypes.Stopped{
		Outcome: result.Outcome, Message: result.Message,
		RetryAfterSeconds: result.RetryAfterSeconds, ResetAt: result.ResetAt,
	})
}

// eventStream writes server-sent events and sends each one on its way at once.
type eventStream struct {
	w       http.ResponseWriter
	flusher *http.ResponseController
}

func newEventStream(w http.ResponseWriter) *eventStream {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	// Tells a proxy in front of the service not to hold the answer back until
	// it is complete.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	stream := &eventStream{w: w, flusher: http.NewResponseController(w)}
	_ = stream.flusher.Flush()
	return stream
}

// send writes one event. A failure to write means the reader has gone, which
// the request's context reports on its own; nothing is done about it here.
func (s *eventStream) send(name string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	// JSON has no raw newline in it, so one data line is always enough.
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, raw)
	_ = s.flusher.Flush()
}
