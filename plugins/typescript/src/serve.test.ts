// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import test from "node:test";
import { parseFrame, PROTOCOL, type Frame } from "./protocol.js";
import { serve, type Analyzer } from "./serve.js";

/** Collects everything written, so a test can assert on frames rather than text. */
class Sink extends Writable {
  chunks: string[] = [];
  override _write(chunk: Buffer, _enc: string, cb: () => void): void {
    this.chunks.push(chunk.toString());
    cb();
  }
  text(): string {
    return this.chunks.join("");
  }
  frames(): Frame[] {
    return this.text()
      .split("\n")
      .filter((l) => l.trim() !== "")
      .map((l) => {
        const parsed = parseFrame(l);
        assert.ok("frame" in parsed, `not a frame: ${l}`);
        return parsed.frame;
      });
  }
}

function converse(analyzers: Analyzer[], ...lines: string[]): Promise<{ out: Sink; log: Sink }> {
  const out = new Sink();
  const log = new Sink();
  const input = Readable.from([lines.join("\n") + "\n"]);
  return serve({ name: "test", version: "0.0.1", analyzers, input, output: out, log }).then(() => ({ out, log }));
}

const noop: Analyzer = {
  id: "demo",
  order: 10,
  lane: "deterministic",
  async run() {
    return { findings: [] };
  },
};

test("full conversation", async () => {
  const { out } = await converse(
    [noop],
    JSON.stringify({ type: "hello", protocol: PROTOCOL, host: "test" }),
    JSON.stringify({ type: "describe" }),
    JSON.stringify({ type: "analyze", analyzer: "demo", request: { root: "/repo", changed: [] } }),
    JSON.stringify({ type: "bye" }),
  );
  const frames = out.frames();
  assert.deepEqual(
    frames.map((f) => f.type),
    ["hello", "describe", "findings"],
  );
  assert.equal(frames[0]?.protocol, PROTOCOL);
  assert.equal(frames[0]?.plugin, "test");
  assert.deepEqual(frames[1]?.analyzers, [{ id: "demo", lane: "deterministic", order: 10, available: true }]);
});

test("refuses a protocol mismatch and says so", async () => {
  const { out } = await converse([noop], JSON.stringify({ type: "hello", protocol: 99 }));
  const frames = out.frames();
  assert.equal(frames.length, 1);
  assert.equal(frames[0]?.type, "error");
  assert.match(frames[0]?.message ?? "", /protocol 99/);
});

test("refuses work before the handshake", async () => {
  const { out } = await converse([noop], JSON.stringify({ type: "analyze", analyzer: "demo" }));
  assert.match(out.frames()[0]?.message ?? "", /before hello/);
});

test("an analyzer that throws does not end the conversation", async () => {
  const broken: Analyzer = {
    id: "broken",
    order: 20,
    lane: "deterministic",
    async run() {
      throw new Error("no configuration found");
    },
  };
  const { out, log } = await converse(
    [broken, noop],
    JSON.stringify({ type: "hello", protocol: PROTOCOL }),
    JSON.stringify({ type: "analyze", analyzer: "broken" }),
    JSON.stringify({ type: "analyze", analyzer: "demo" }),
    JSON.stringify({ type: "bye" }),
  );
  const frames = out.frames();
  assert.deepEqual(
    frames.map((f) => f.type),
    ["hello", "error", "findings"],
  );
  assert.equal(frames[1]?.analyzer, "broken");
  assert.match(log.text(), /no configuration found/);
});

test("an unavailable analyzer is described, not hidden", async () => {
  const absent: Analyzer = {
    id: "absent",
    order: 30,
    lane: "deterministic",
    async unavailable() {
      return "no eslint configuration found";
    },
    async run() {
      return { findings: [] };
    },
  };
  const { out } = await converse(
    [absent],
    JSON.stringify({ type: "hello", protocol: PROTOCOL }),
    JSON.stringify({ type: "describe" }),
    JSON.stringify({ type: "bye" }),
  );
  const [descriptor] = out.frames()[1]?.analyzers ?? [];
  assert.equal(descriptor?.available, false);
  assert.equal(descriptor?.unavailable, "no eslint configuration found");
});

test("a line that is not a frame is reported and the loop continues", async () => {
  const { out, log } = await converse(
    [noop],
    JSON.stringify({ type: "hello", protocol: PROTOCOL }),
    "Debugger listening on ws://127.0.0.1:9229",
    JSON.stringify({ type: "bye" }),
  );
  const frames = out.frames();
  assert.deepEqual(
    frames.map((f) => f.type),
    ["hello", "error"],
  );
  assert.match(log.text(), /not a protocol frame/);
});

test("unknown frame types are ignored", async () => {
  const { out, log } = await converse(
    [noop],
    JSON.stringify({ type: "hello", protocol: PROTOCOL }),
    JSON.stringify({ type: "prefetch" }),
    JSON.stringify({ type: "bye" }),
  );
  assert.equal(out.frames().length, 1);
  assert.match(log.text(), /unknown frame type/);
});

test("an unknown analyzer id is an error frame, not a crash", async () => {
  const { out } = await converse(
    [noop],
    JSON.stringify({ type: "hello", protocol: PROTOCOL }),
    JSON.stringify({ type: "analyze", analyzer: "nope" }),
    JSON.stringify({ type: "bye" }),
  );
  assert.match(out.frames()[1]?.message ?? "", /no analyzer named nope/);
});

test("parseFrame rejects what is not a frame", () => {
  for (const [input, want] of [
    ["not json", /not a protocol frame/],
    ["[1,2,3]", /not a JSON object/],
    ['{"protocol":1}', /no type/],
  ] as const) {
    const parsed = parseFrame(input);
    assert.ok("error" in parsed, `${input} should not parse`);
    assert.match(parsed.error, want);
  }
});
