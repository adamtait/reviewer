// SPDX-License-Identifier: MIT

import { type SyntheticEvent, useEffect, useRef, useState } from 'react';

import type { Excerpt } from '../api/types';
import type { Conversation, Exchange, Quoted } from '../nav/conversation';
import { href } from '../router';
import { Link } from './Link';
import { Markdown } from './Markdown';

export interface AssistantPanelProps {
  owner: string;
  repo: string;
  /** The commit the next question is asked about. */
  sha: string;
  pull?: number | undefined;
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

/** The lines of an excerpt, as a person counts them. */
function lines(excerpt: { startLine: number; endLine: number }): string {
  const first = excerpt.startLine + 1;
  return excerpt.endLine > first ? `lines ${first} to ${excerpt.endLine}` : `line ${first}`;
}

export function AssistantPanel({ owner, repo, sha, pull, github, conversation, selection, onClearSelection, onClose }: AssistantPanelProps) {
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
    conversation.ask(selection?.sha ?? sha, text, selection);
    setQuestion('');
    onClearSelection();
  };

  const excerptLink = (excerpt: Excerpt) =>
    href({ view: 'code', owner, repo, sha: excerpt.sha, path: excerpt.path, line: excerpt.startLine + 1, ...(pull ? { pull } : {}) });

  return (
    <section className="assistant" aria-label="Assistant">
      <header className="row">
        <h2>Ask about this code</h2>
        <button type="button" onClick={onClose}>
          Close
        </button>
      </header>
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
                About {exchange.selection.path}, {lines(exchange.selection)}.
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
                <Markdown text={exchange.answer} trusted={github ? [github] : []} />
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

      <form onSubmit={send} className="ask-form">
        {selection ? (
          <div className="quoted" data-testid="quoted">
            <div className="row">
              <span className="match-place">
                {selection.path}, {lines(selection)}
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

/** The code an answer rests on: every run of lines the model was given, each a link to it. */
function Excerpts({ exchange, link }: { exchange: Exchange; link: (excerpt: Excerpt) => string }) {
  // The same lines can be given twice, by two tools. Listed once.
  const seen = new Set<string>();
  const unique = exchange.excerpts.filter((excerpt) => {
    const key = `${excerpt.sha}:${excerpt.path}:${excerpt.startLine}:${excerpt.endLine}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  return (
    <details className="excerpts" open={exchange.state !== 'asking'}>
      <summary>
        The model was given {unique.length} {unique.length === 1 ? 'excerpt' : 'excerpts'}
      </summary>
      <ul>
        {unique.map((excerpt) => (
          <li key={`${excerpt.sha}:${excerpt.path}:${excerpt.startLine}:${excerpt.endLine}`}>
            <Link to={link(excerpt)}>
              {excerpt.path}, {lines(excerpt)}
            </Link>
          </li>
        ))}
      </ul>
    </details>
  );
}
