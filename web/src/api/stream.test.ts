// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiFailure } from './client';
import { EventParser, StreamCut, ask } from './stream';
import type { AnswerEvent, Ask } from './types';

// The streams the service's own tests record. A field the service renames
// changes these files, and the checks on their fields below then fail.
// Read by path from the web directory, where the tests are run.
const example = (name: string) =>
  readFileSync(resolve(process.cwd(), '..', 'internal', 'workspace', 'api', 'testdata', 'examples', name), 'utf8');
const recorded = example('question.sse');
const stopped = example('question_stopped.sse');

const sha = '5'.repeat(40);
const question: Ask = { text: 'What does add return?', pullNumber: 12, history: [], selection: null, path: '' };

afterEach(() => {
  vi.unstubAllGlobals();
});

/** A response whose body arrives in the given pieces. */
function streamed(pieces: string[], status = 200): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const piece of pieces) controller.enqueue(encoder.encode(piece));
      controller.close();
    },
  });
  return new Response(body, { status, headers: { 'Content-Type': 'text/event-stream' } });
}

function serve(response: Response) {
  const fetched = vi.fn<typeof fetch>(() => Promise.resolve(response));
  vi.stubGlobal('fetch', fetched);
  return fetched;
}

async function collect(): Promise<AnswerEvent[]> {
  const events: AnswerEvent[] = [];
  await ask('example', 'alpha', sha, question, (event) => events.push(event));
  return events;
}

describe('the recorded stream', () => {
  it('is read into the events the service sent, in order', async () => {
    serve(streamed([recorded]));
    const events = await collect();
    expect(events.map((e) => e.event)).toEqual(['excerpt', 'excerpt', 'delta', 'done']);
    expect(events[0]).toEqual({ event: 'excerpt', data: { sha, path: 'src/cart.ts', startLine: 12, endLine: 17 } });
    const last = events.at(-1);
    expect(last?.event).toBe('done');
    if (last?.event === 'done') {
      // Every field the type names is in the recorded event, and no other.
      expect(Object.keys(last.data).sort()).toEqual(['inputTokens', 'model', 'modelCalls', 'outputTokens', 'reasoningTokens']);
    }
  });

  it('is read the same however it is cut into pieces', async () => {
    for (const size of [1, 7, 64, 1000]) {
      const pieces: string[] = [];
      for (let i = 0; i < recorded.length; i += size) pieces.push(recorded.slice(i, i + size));
      serve(streamed(pieces));
      expect((await collect()).map((e) => e.event), `pieces of ${size}`).toEqual(['excerpt', 'excerpt', 'delta', 'done']);
    }
  });

  it('ends with the reason when a question could not be sent', async () => {
    serve(streamed([stopped]));
    const events = await collect();
    expect(events).toHaveLength(1);
    const [only] = events;
    expect(only?.event).toBe('stopped');
    if (only?.event === 'stopped') {
      expect(only.data.outcome).toBe('secret_blocked');
      expect(Object.keys(only.data).sort()).toEqual(['message', 'outcome', 'resetAt', 'retryAfterSeconds']);
    }
  });
});

describe('asking', () => {
  it('sends the question in the body, with the header the service requires', async () => {
    const fetched = serve(streamed([recorded]));
    await collect();
    const [address, init] = fetched.mock.calls[0] ?? [];
    expect(address).toBe(`/api/v1/repos/example/alpha/commits/${sha}/questions`);
    expect(init?.method).toBe('POST');
    expect((init?.headers as Record<string, string>)['X-Workspace-Request']).toBe('1');
    expect(JSON.parse(init?.body as string)).toEqual(question);
    // Nothing of the question is in the address.
    expect(typeof address === 'string' && !address.includes('add')).toBe(true);
  });

  it('rejects with the service\'s reason when the question is refused before an answer begins', async () => {
    serve(
      new Response(JSON.stringify({ error: { code: 'no_assistant', message: 'This repository has no assistant configured.' } }), {
        status: 404,
      }),
    );
    await expect(collect()).rejects.toMatchObject({ code: 'no_assistant', status: 404 });
    serve(new Response('<html>bad gateway</html>', { status: 502 }));
    await expect(collect()).rejects.toBeInstanceOf(ApiFailure);
  });

  it('says so when the stream ends without its ending', async () => {
    const cut = recorded.slice(0, recorded.indexOf('event: done'));
    serve(streamed([cut]));
    await expect(collect()).rejects.toBeInstanceOf(StreamCut);
    serve(streamed([]));
    await expect(collect()).rejects.toBeInstanceOf(StreamCut);
  });
});

describe('the event parser', () => {
  it('keeps half an event until the rest arrives', () => {
    const parser = new EventParser();
    expect(parser.feed('event: delta\ndata: {"te')).toEqual([]);
    expect(parser.unfinished).toBe(true);
    expect(parser.feed('xt":"hello"}\n\n')).toEqual([{ event: 'delta', data: { text: 'hello' } }]);
    expect(parser.unfinished).toBe(false);
  });

  it('ignores comments, unknown events and data that is not JSON', () => {
    const parser = new EventParser();
    const events = parser.feed(
      ': a comment\n\nevent: something_new\ndata: {}\n\nevent: delta\ndata: {not json\n\nevent: delta\r\ndata: {"text":"ok"}\r\n\r\n',
    );
    expect(events).toEqual([{ event: 'delta', data: { text: 'ok' } }]);
  });
});
