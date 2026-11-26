// SPDX-License-Identifier: MIT

import { type SyntheticEvent, useEffect, useRef, useState } from 'react';

import { repositoryUrl } from '../github/links';
import type { Conversation, Exchange, Quoted } from '../nav/conversation';
import { type FileExcerpts, type Run, brief, byFile, spoken } from '../nav/excerpts';
import { href } from '../router';
import { Link } from './Link';
import { Markdown } from './Markdown';

export interface AssistantPanelProps {
  owner: string;
  repo: string;
  /** The commit the next question is asked about. */
  sha: string;
  pull?: number | undefined;
  /**
   * The file the reader has open, if the view shows one file. The model is
   * told its name, so that "this file" in a question means something.
   */
  path?: string | undefined;
  /** The GitHub the workspace reads from, so that a link to it in an answer can be followed. */
  github?: string | undefined;
  conversation: Conversation;
  /** The code the reader chose to ask about, quoted above the question. */
  selection: Quoted | null;
  onClearSelection: () => void;
  onClose: () => void;
}

// What the reader is told for each way an answer ends without being completed.
// The service's own message follows, with the particulars.
const stoppedTitles: Record<string, string> = {
  rate_limited: 'The model provider is limiting requests',
  quota_exhausted: 'The model provider’s quota is exhausted',
  limit_reached: 'The assistant reached one of its limits',
  secret_blocked: 'Not sent: the secrets scan found a credential',
  scan_failed: 'Not sent: the secrets scan could not run',
  refused: 'The model provider declined to answer',
  provider_error: 'The model provider returned an error',
};

// A list of this many files or fewer is shown open under its answer. A longer
// one is closed until asked for: the answer is what the reader came for.
const openUpTo = 6;

export function AssistantPanel({
  owner,
  repo,
  sha,
  pull,
  path,
  github,
  conversation,
  selection,
  onClearSelection,
  onClose,
}: AssistantPanelProps) {
  const [question, setQuestion] = useState('');
  const box = useRef<HTMLTextAreaElement>(null);
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    box.current?.focus();
  }, [selection]);

  // Keep the newest part of the answer in view as it arrives.
  const last = conversation.exchanges.at(-1);
  useEffect(() => {
    end.current?.scrollIntoView({ block: 'nearest' });
  }, [last?.answer, last?.state]);

  const send = (event?: SyntheticEvent) => {
    event?.preventDefault();
    const text = question.trim();
    if (text === '' || conversation.busy) return;
    conversation.ask(selection?.sha ?? sha, text, selection, path ?? '');
    setQuestion('');
    onClearSelection();
  };

  const excerptLink = (file: FileExcerpts, run: Run) =>
    href({ view: 'code', owner, repo, sha: file.sha, path: file.path, line: run.startLine + 1, ...(pull ? { pull } : {}) });

  return (
    <section className="assistant" aria-label="Assistant">
      <header className="row">
        <h2>Ask about this code</h2>
        <button type="button" onClick={onClose}>
          Close
        </button>
      </header>
      {/* The conversation scrolls in a box of its own, above the question
          box, so that the end of an answer is never underneath it. */}
      <div className="conversation">
        <p className="muted">
          Questions go to the model provider configured for this repository, with the code the assistant reads to answer
          them. The conversation is kept in this page only, and is gone on reload.
        </p>

        <ol className="exchanges" aria-live="polite">
          {conversation.exchanges.map((exchange) => (
            <li key={exchange.id} className={`exchange exchange-${exchange.state}`}>
              <p className="asked">{exchange.question}</p>
              {exchange.selection ? (
                <p className="muted asked-about">
                  About {exchange.selection.path}, {spoken(exchange.selection)}.
                </p>
              ) : null}

              {exchange.waits.map((wait, i) => (
                <p key={i} className="notice" role="status">
                  {wait.reason} Waiting {wait.seconds} seconds, then trying once more.
                </p>
              ))}
              {exchange.withheld.map((held) => (
                <p key={held.what} className="notice withheld" role="status">
                  <strong>Withheld from the model: {held.what}.</strong> {held.reason}
                </p>
              ))}

              {exchange.answer ? (
                <div className="answer" data-testid="answer">
                  <Markdown text={exchange.answer} trusted={github ? [github] : []} repository={github ? repositoryUrl(github) : undefined} />
                </div>
              ) : null}
              {exchange.state === 'asking' ? (
                <p className="muted" role="status">
                  {exchange.answer ? 'Writing…' : 'Reading the code…'}
                </p>
              ) : null}

              {exchange.state === 'stopped' && exchange.stopped ? (
                <div className="stopped" role="alert" data-outcome={exchange.stopped.outcome}>
                  <strong>{stoppedTitles[exchange.stopped.outcome] ?? 'The answer was stopped'}.</strong>
                  <p>{exchange.stopped.message}</p>
                </div>
              ) : null}
              {exchange.state === 'failed' ? (
                <p className="error" role="alert">
                  {exchange.failure}
                </p>
              ) : null}

              {exchange.excerpts.length > 0 ? <Excerpts exchange={exchange} link={excerptLink} /> : null}
              {exchange.done ? (
                <p className="muted model" data-testid="answered-by">
                  Answered by {exchange.done.model}, in {exchange.done.modelCalls} model{' '}
                  {exchange.done.modelCalls === 1 ? 'call' : 'calls'}. An answer can be wrong; the code it rests on is listed
                  above.
                </p>
              ) : null}
            </li>
          ))}
        </ol>
        <div ref={end} />
      </div>

      <form onSubmit={send} className="ask-form">
        {selection ? (
          <div className="quoted" data-testid="quoted">
            <div className="row">
              <span className="match-place">
                {selection.path}, {spoken(selection)}
              </span>
              <button type="button" onClick={onClearSelection}>
                Ask without it
              </button>
            </div>
            <pre>
              <code>{selection.text}</code>
            </pre>
          </div>
        ) : null}
        <label>
          <span className="label">Your question</span>
          <textarea
            ref={box}
            rows={3}
            value={question}
            placeholder={selection ? 'What does this do?' : 'Ask about this commit'}
            onChange={(event) => {
              setQuestion(event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) send(event);
            }}
          />
        </label>
        <div className="row">
          {conversation.busy ? (
            <button type="button" onClick={conversation.stop}>
              Stop
            </button>
          ) : (
            <button type="submit" disabled={question.trim() === ''}>
              Ask
            </button>
          )}
          <span className="muted hint">Ctrl or Cmd with Enter sends.</span>
        </div>
      </form>
    </section>
  );
}

/**
 * The code an answer rests on: the lines the model was given, by file, each
 * run a link to it.
 */
function Excerpts({ exchange, link }: { exchange: Exchange; link: (file: FileExcerpts, run: Run) => string }) {
  const files = byFile(exchange.excerpts);
  return (
    <details className="excerpts" open={exchange.state !== 'asking' && files.length <= openUpTo}>
      <summary>
        The model was given lines from {files.length} {files.length === 1 ? 'file' : 'files'}
      </summary>
      <ul>
        {files.map((file) => (
          <li key={`${file.sha}:${file.path}`}>
            <span className="excerpt-path">{file.path}</span>{' '}
            <span className="excerpt-runs">
              {file.runs.map((run, i) => (
                <span key={run.startLine}>
                  {i > 0 ? ', ' : ''}
                  {/* Named in full for a reader who hears the link and not the row it sits on. */}
                  <Link to={link(file, run)} label={`${file.path}, ${run.endLine > run.startLine + 1 ? 'lines' : 'line'} ${brief(run)}`}>
                    {brief(run)}
                  </Link>
                </span>
              ))}
            </span>
          </li>
        ))}
      </ul>
    </details>
  );
}
