// SPDX-License-Identifier: MIT

import { useCallback, useRef, useState } from 'react';

import { ApiFailure } from '../api/client';
import { StreamCut, ask } from '../api/stream';
import type { AskMessage, AskSelection, Done, Excerpt, Stopped, Waiting, Withheld } from '../api/types';

// The conversation with the assistant.
//
// It lives in the page and nowhere else. The service keeps nothing between
// questions, so each question carries the earlier questions and answers with
// it, as plain text. A reload loses the conversation, and a dropped connection
// loses the answer in progress.

/** The code a question is about, with its text for showing above the question. */
export interface Quoted extends AskSelection {
  text: string;
}

/** One question and what came back. */
export interface Exchange {
  id: number;
  sha: string;
  question: string;
  selection: Quoted | null;
  answer: string;
  /** Every run of lines the model was given, in the order it was given them. */
  excerpts: Excerpt[];
  withheld: Withheld[];
  /** The waits the assistant sat out, each as the provider named it. */
  waits: Waiting[];
  state: 'asking' | 'done' | 'stopped' | 'failed';
  done?: Done;
  stopped?: Stopped;
  /** Why the question failed, when it failed outside the answer's own stream. */
  failure?: string;
}

/**
 * What is sent with the next question: the questions that were answered, each
 * with its answer. A question that was stopped or failed is left out, having
 * no answer to carry.
 */
export function history(exchanges: readonly Exchange[]): AskMessage[] {
  return exchanges
    .filter((exchange) => exchange.state === 'done' && exchange.answer.trim() !== '')
    .flatMap((exchange): AskMessage[] => [
      { role: 'user', text: exchange.question },
      { role: 'assistant', text: exchange.answer },
    ]);
}

export interface Conversation {
  exchanges: Exchange[];
  /** Whether an answer is arriving. */
  busy: boolean;
  ask: (sha: string, question: string, selection: Quoted | null) => void;
  /** Stops the answer that is arriving. The service stops with it. */
  stop: () => void;
}

/** Holds the conversation for one view, and asks. */
export function useConversation(owner: string, repo: string, pull: number | undefined): Conversation {
  const [exchanges, setExchanges] = useState<Exchange[]>([]);
  const [busy, setBusy] = useState(false);
  const running = useRef<AbortController | null>(null);
  const next = useRef(1);
  const latest = useRef<Exchange[]>([]);
  latest.current = exchanges;

  const stop = useCallback(() => {
    running.current?.abort();
  }, []);

  const start = useCallback(
    (sha: string, question: string, selection: Quoted | null) => {
      if (running.current) return;
      const id = next.current++;
      const earlier = history(latest.current);
      const controller = new AbortController();
      running.current = controller;
      setBusy(true);
      setExchanges((all) => [
        ...all,
        { id, sha, question, selection, answer: '', excerpts: [], withheld: [], waits: [], state: 'asking' },
      ]);
      const change = (update: (exchange: Exchange) => Exchange) => {
        setExchanges((all) => all.map((exchange) => (exchange.id === id ? update(exchange) : exchange)));
      };

      ask(
        owner,
        repo,
        sha,
        {
          text: question,
          pullNumber: pull ?? 0,
          history: earlier,
          selection: selection ? { sha: selection.sha, path: selection.path, startLine: selection.startLine, endLine: selection.endLine } : null,
        },
        (event) => {
          switch (event.event) {
            case 'delta':
              change((e) => ({ ...e, answer: e.answer + event.data.text }));
              break;
            case 'excerpt':
              change((e) => ({ ...e, excerpts: [...e.excerpts, event.data] }));
              break;
            case 'withheld':
              change((e) => ({ ...e, withheld: [...e.withheld, event.data] }));
              break;
            case 'waiting':
              change((e) => ({ ...e, waits: [...e.waits, event.data] }));
              break;
            case 'done':
              change((e) => ({ ...e, state: 'done', done: event.data }));
              break;
            case 'stopped':
              change((e) => ({ ...e, state: 'stopped', stopped: event.data }));
              break;
          }
        },
        controller.signal,
      )
        .catch((error: unknown) => {
          let failure = 'The question could not be asked.';
          if (controller.signal.aborted) failure = 'Stopped.';
          else if (error instanceof ApiFailure) failure = error.message;
          else if (error instanceof StreamCut) failure = 'The connection dropped before the answer was complete.';
          change((e) => (e.state === 'asking' ? { ...e, state: 'failed', failure } : e));
        })
        .finally(() => {
          running.current = null;
          setBusy(false);
        });
    },
    [owner, repo, pull],
  );

  return { exchanges, busy, ask: start, stop };
}
