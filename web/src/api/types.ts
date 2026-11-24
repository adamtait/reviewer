// SPDX-License-Identifier: MIT

// The bodies of the workspace's HTTP API.
//
// Written by hand to mirror internal/workspace/api/apitypes/types.go. What
// keeps the two the same is the set of recorded responses under
// internal/workspace/api/testdata/examples: the Go handler tests must produce
// them, examples.test.ts checks them against these types, and the journeys in
// e2e/ are served them.
//
// In every body: `line` counts from 0.

export interface ApiError {
  error: { code: ErrorCode | (string & {}); message: string };
}

export type ErrorCode =
  | 'bad_request'
  | 'signed_out'
  | 'not_found'
  | 'too_large'
  | 'github_failed'
  | 'github_timeout'
  | 'internal'
  | 'not_fine_grained'
  | 'rejected'
  | 'no_repository';

export interface RepositoryRef {
  owner: string;
  name: string;
}

export interface Me {
  login: string;
  repositories: RepositoryRef[];
}

export interface Health {
  status: string;
  version: string;
}

export interface PullSummary {
  number: number;
  title: string;
  author: string;
  draft: boolean;
  headSha: string;
  /** RFC 3339, in UTC. */
  updatedAt: string;
}

export interface PullList {
  pulls: PullSummary[];
  truncated: boolean;
}

export type ChangeStatus = 'added' | 'modified' | 'deleted' | 'renamed';

export interface ChangedFile {
  path: string;
  /** Set for a renamed file: where its old version is read from, at the merge base. */
  previousPath: string;
  status: ChangeStatus;
}

export interface PullView {
  number: number;
  title: string;
  /** As its author wrote it. Untrusted text: shown as text, never as HTML. */
  body: string;
  author: string;
  state: string;
  draft: boolean;
  headSha: string;
  baseRef: string;
  mergeBaseSha: string;
  files: ChangedFile[];
  filesTruncated: boolean;
  webUrl: string;
}

export type EntryKind = 'text' | 'binary' | 'too_large' | 'other';

export interface TreeEntry {
  path: string;
  kind: EntryKind;
}

export interface Tree {
  entries: TreeEntry[];
}

export interface File {
  path: string;
  kind: EntryKind;
  /** Empty unless kind is "text". */
  text: string;
}

export interface Match {
  path: string;
  line: number;
  text: string;
}

export interface Search {
  query: string;
  pathPrefix: string;
  caseSensitive: boolean;
}

export interface SearchResult {
  matches: Match[];
  truncated: boolean;
}

export type IndexStateName = 'none' | 'running' | 'failed' | 'ready';

export interface IndexState {
  state: IndexStateName;
  /** Why there is no index although the index job succeeded. Empty otherwise. */
  reason: string;
  /** The workflow run on GitHub the state rests on. Empty when there is none. */
  runUrl: string;
  toolVersion: string;
  documents: number;
}

/** A span in a file. Lines count from 0, the end is exclusive, characters are UTF-16 code units. */
export interface Location {
  path: string;
  line: number;
  character: number;
  endLine: number;
  endCharacter: number;
}

/**
 * Where a navigation result came from. A label names the source. It does not
 * say the result is correct: the index is built by CI and cannot be verified.
 */
export type Label = 'index' | 'text_search' | 'external' | 'none';

export interface Definition {
  label: Label;
  /** Filled when the label is "index". */
  targets: Location[];
  /** Filled when the label is "external". */
  package: string;
  version: string;
  /** Filled when the label is "text_search". */
  matches: Match[];
  truncated: boolean;
}

export interface References {
  label: Label;
  references: Location[];
  truncated: boolean;
  /** Always given beside the index's list: an index can cover part of a repository. */
  matches: Match[];
  matchesTruncated: boolean;
}

export type LookupKind = 'definition' | 'references';

/** A user saying a navigation result was wrong. It names the lookup and carries no text. */
export interface Report {
  sha: string;
  path: string;
  line: number;
  character: number;
  kind: LookupKind;
  label: Label;
}
