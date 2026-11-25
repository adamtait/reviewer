// SPDX-License-Identifier: MIT

import { api } from '../api/client';
import type { Definition, Label, Location, LookupKind, References, Report } from '../api/types';
import type { Side } from '../github/links';
import { href, navigate } from '../router';

// Go-to-definition and find-references.
//
// Every result carries a label saying where it came from, and the label is
// shown wherever the result is. "From the CI-built index" names a source; it
// does not say the result is right, because the index is built by a job the
// pull request's author controls.

/** What each label is called on screen. */
export const labelText: Record<Label, string> = {
  index: 'From the CI-built index',
  text_search: 'From text search',
  external: 'Defined outside the repository',
  none: 'Not found',
};

/** A place a lookup is asked about: one character of one line of one side of a file. */
export interface Origin {
  side: Side;
  path: string;
  line: number;
  character: number;
}

/**
 * The commit a lookup is made at.
 *
 * A removed line exists only in the old version of its file, so a lookup from
 * one uses the merge base. Any other lookup uses the head commit. The merge
 * base has an index only if it was once the tip of a push; when it has none,
 * the lookup falls back to text search like any other.
 */
export function lookupCommit(pull: { headSha: string; mergeBaseSha: string }, side: Side): string {
  return side === 'old' ? pull.mergeBaseSha : pull.headSha;
}

/** Where a lookup happens and where its results lead. */
export interface Context {
  owner: string;
  repo: string;
  /** The commit the lookup is made at, and that every target is in. */
  sha: string;
  /** The pull request the reader is in, if any, so a result can lead back to it. */
  pull?: number | undefined;
}

/** How a page was reached by a lookup. It travels with the history entry, not in the address. */
export interface Arrival {
  label: Label;
  kind: LookupKind;
  /** The lookup that led here, for a "this was wrong" report. */
  origin: { sha: string; path: string; line: number; character: number };
}

/** Reads how the current page was reached, if it was reached by a lookup. */
export function arrival(state: unknown): Arrival | null {
  if (typeof state !== 'object' || state === null || !('arrival' in state)) return null;
  const value: unknown = state.arrival;
  if (typeof value !== 'object' || value === null) return null;
  const a = value as Partial<Arrival>;
  if (typeof a.label !== 'string' || typeof a.kind !== 'string' || typeof a.origin !== 'object') return null;
  return a as Arrival;
}

/** The address of a place a lookup found. */
export function targetHref(context: Context, at: { path: string; line: number }): string {
  return href({
    view: 'code',
    owner: context.owner,
    repo: context.repo,
    sha: context.sha,
    path: at.path,
    line: at.line + 1,
    ...(context.pull ? { pull: context.pull } : {}),
  });
}

/** Goes to a place a lookup found, remembering how it was reached. */
export function goTo(context: Context, at: Location | { path: string; line: number }, how: Arrival): void {
  navigate(targetHref(context, at), { state: { arrival: how } });
}

/** What the page should do with a definition lookup's answer. */
export type DefinitionOutcome =
  /** One target from the index: go there. */
  | { action: 'jump'; target: Location }
  /** Anything else is shown and not followed: several targets, text matches, a package, or nothing. */
  | { action: 'show'; result: Definition };

/**
 * Decides what a definition lookup's answer leads to.
 *
 * Only a single target from the index is followed without asking. Text
 * matches are lines that contain the word, not definitions, and are listed
 * under their own label for the reader to choose from.
 */
export function definitionOutcome(result: Definition): DefinitionOutcome {
  const [only, ...others] = result.targets;
  if (result.label === 'index' && only && others.length === 0) {
    return { action: 'jump', target: only };
  }
  return { action: 'show', result };
}

export async function findDefinition(context: Context, origin: Origin, signal?: AbortSignal): Promise<Definition> {
  return api.definition(context.owner, context.repo, context.sha, origin.path, origin.line, origin.character, signal);
}

export async function findReferences(context: Context, origin: Origin, signal?: AbortSignal): Promise<References> {
  return api.references(context.owner, context.repo, context.sha, origin.path, origin.line, origin.character, signal);
}

/** Builds the report for a result the reader says was wrong. */
export function reportFor(how: Arrival): Report {
  return { ...how.origin, kind: how.kind, label: how.label };
}

export async function sendReport(context: Pick<Context, 'owner' | 'repo'>, how: Arrival): Promise<void> {
  await api.report(context.owner, context.repo, reportFor(how));
}
