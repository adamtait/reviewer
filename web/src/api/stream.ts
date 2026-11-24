// SPDX-License-Identifier: MIT

import { ApiFailure } from './client';
import type { AnswerEvent, ApiError, Ask } from './types';

// Reading an answer as it is written.
//
// The question route answers with server-sent events over the response to a
// POST. The browser's own EventSource cannot be used: it only makes GET
// requests, and the question, the selection and the conversation travel in
// the body, where they are not in any address or log.

const names = new Set(['delta', 'excerpt', 'withheld', 'waiting', 'done', 'stopped']);

/**
 * Splits a stream of text into server-sent events. Text arrives in pieces
 * that end anywhere, so what is not yet a whole event is kept for the next
 * piece.
 */
export class EventParser {
  private rest = '';

  /** Takes the next piece of text and returns the events it completes. */
  feed(piece: string): AnswerEvent[] {
    this.rest += piece.replace(/\r\n/g, '\n');
    const out: AnswerEvent[] = [];
    for (;;) {
      const end = this.rest.indexOf('\n\n');
      if (end < 0) return out;
      const block = this.rest.slice(0, end);
      this.rest = this.rest.slice(end + 2);
      const parsed = parseBlock(block);
      if (parsed) out.push(parsed);
    }
  }

  /** Whether text is left over that never became an event: the stream was cut. */
  get unfinished(): boolean {
    return this.rest.trim() !== '';
  }
}

function parseBlock(block: string): AnswerEvent | null {
  let name = '';
  const data: string[] = [];
  for (const line of block.split('\n')) {
    if (line.startsWith('event:')) name = line.slice(6).trim();
    else if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
    // A line starting with a colon is a comment, and anything else is ignored.
  }
  if (!names.has(name) || data.length === 0) return null;
  try {
    return { event: name, data: JSON.parse(data.join('\n')) as unknown } as AnswerEvent;
  } catch {
    return null;
  }
}

/** What reading an answer ended with, when the stream itself went wrong. */
export class StreamCut extends Error {
  constructor() {
    super('The answer was cut off before it was complete.');
    this.name = 'StreamCut';
  }
}

/**
 * Asks a question and calls `onEvent` for each event of the answer, in order.
 *
 * It resolves once the stream has ended with `done` or `stopped`. It rejects
 * with an ApiFailure if the question was refused before any answer began, and
 * with a StreamCut if the stream ended without either of its two endings. An
 * aborted signal stops the question on the service too: the request ends, and
 * the service stops with it.
 */
export async function ask(
  owner: string,
  repo: string,
  sha: string,
  question: Ask,
  onEvent: (event: AnswerEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const init: RequestInit = {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', 'X-Workspace-Request': '1' },
    body: JSON.stringify(question),
  };
  if (signal) init.signal = signal;
  const response = await fetch(
    `/api/v1/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/commits/${sha}/questions`,
    init,
  );

  if (!response.ok) {
    let failure: Partial<ApiError> = {};
    try {
      failure = (await response.json()) as Partial<ApiError>;
    } catch {
      // Not JSON: say what is known.
    }
    throw new ApiFailure(response.status, failure.error?.code ?? 'internal', failure.error?.message ?? 'The question could not be asked.');
  }
  if (!response.body) throw new StreamCut();

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = new EventParser();
  let ended = false;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    for (const event of parser.feed(decoder.decode(value, { stream: true }))) {
      onEvent(event);
      if (event.event === 'done' || event.event === 'stopped') ended = true;
    }
  }
  if (!ended) throw new StreamCut();
}
