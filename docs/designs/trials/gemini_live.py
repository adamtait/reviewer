#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Trial tool: one live tool-calling conversation with the Gemini API.

Asks a question that needs two tool calls in sequence (search, then read a
file) against a two-file made-up repository, streams the answer, and prints
what the review workspace design needs to know: which kinds of step arrive,
whether the provider's reasoning data must be sent back, and the token counts.

  GEMINI_API_KEY=... gemini_live.py MODE MODEL

  MODE  new        the Interactions API, stateless (store: false), streamed
        new-drop   the same, with reasoning steps left out of the second request
        old        generateContent, streamed
        old-drop   the same, with thought signatures left out

Set PACE_SECONDS to wait before each call when the key has a low rate limit.
The key is read from the environment and never printed. Standard library only.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.request

KEY = os.environ.get("GEMINI_API_KEY", "")
PACE = float(os.environ.get("PACE_SECONDS", "0"))  # seconds to wait before each call, for low rate limits
BASE = "https://generativelanguage.googleapis.com/v1beta"
REPO = {"src/intent.ts": "export interface Intent { goal: string; plan: string }\n\nexport function parseIntent(body: string): Intent | null {\n  const m = body.match(/<!-- review-intent v1 -->([\\s\\S]*?)<!-- \\/review-intent -->/);\n  if (!m) return null;\n  return { goal: field(m[1], 'Goal'), plan: field(m[1], 'Plan') };\n}\n\nfunction field(block: string, label: string): string {\n  const m = block.match(new RegExp('\\\\*\\\\*' + label + ':\\\\*\\\\* (.*)'));\n  return m ? m[1].trim() : '';\n}\n",
        "src/view.ts": "import { parseIntent } from './intent';\nexport function render(body: string) { return parseIntent(body)?.goal ?? 'none'; }\n"}
SYSTEM = "You answer questions about a code repository. You have no knowledge of it. Use the tools to search and to read files before answering. Read a file before describing it."
QUESTION = "Where is parseIntent defined, and what does it return when the description has no intent block? Search first, then read the file."
def run_tool(name, args):
    if name == "search_text":
        q = args.get("query", "")
        hits = [f"{p}:{i+1}: {l}" for p, t in REPO.items() for i, l in enumerate(t.split("\n")) if q.lower() in l.lower()]
        return "\n".join(hits) or "no matches"
    if name == "read_file":
        return REPO.get(args.get("path", ""), "no such file")
    return "unknown tool"
def post(url, body, stream):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers={"x-goog-api-key": KEY, "Content-Type": "application/json"})
    time.sleep(PACE)
    t0 = time.monotonic()
    try:
        resp = urllib.request.urlopen(req, timeout=120)
    except urllib.error.HTTPError as e:
        hdr = {k.lower(): v for k, v in e.headers.items()}
        print("   error headers:", {k: hdr[k] for k in hdr if k in ("retry-after", "content-type") or k.startswith("x-ratelimit")})
        return e.code, e.read().decode(errors="replace"), dict(e.headers), None
    if not stream:
        return resp.status, resp.read().decode(), dict(resp.headers), None
    events, first = [], None
    name = None
    for raw in resp:
        line = raw.decode().rstrip("\n")
        if line.startswith("event:"): name = line[6:].strip()
        elif line.startswith("data:"):
            data = line[5:].strip()
            if first is None: first = time.monotonic() - t0
            if data != "[DONE]":
                events.append((name, json.loads(data)))
    return resp.status, events, dict(resp.headers), first

# ---- A: Interactions API, stateless, streaming -----------------------------
ITOOLS = [{"type": "function", "name": "search_text", "description": "Search the repository for a literal string. Returns matching lines as path:line: text.", "parameters": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}},
          {"type": "function", "name": "read_file", "description": "Read one file of the repository.", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}]
def interactions(model, drop_thoughts=False, maxout=None):
    history = [{"type": "user_input", "content": QUESTION}]
    log = []
    for rnd in range(6):
        body = {"model": model, "store": False, "stream": True, "input": history, "tools": ITOOLS, "system_instruction": SYSTEM}
        if maxout: body["generation_config"] = {"max_output_tokens": maxout}
        status, events, headers, first = post(f"{BASE}/interactions", body, True)
        if status != 200:
            log.append({"round": rnd + 1, "status": status, "error": events[:700]}); break
        steps, order, usage, final_status = {}, [], None, None
        kinds = []
        for name, ev in events:
            et = ev.get("event_type")
            if et == "step.start":
                steps[ev["index"]] = dict(ev["step"]); order.append(ev["index"])
                if ev["step"].get("type") == "function_call": steps[ev["index"]]["_args"] = ""
            elif et == "step.delta":
                d = ev["delta"]; st = steps[ev["index"]]; kinds.append(d.get("type"))
                if d["type"] == "text": st.setdefault("content", [{"type": "text", "text": ""}]); st["content"][0]["text"] += d["text"]
                elif d["type"] == "thought_signature": st["signature"] = d["signature"]
                elif d["type"] == "arguments_delta": st["_args"] += d["arguments"]
                else: st.setdefault("_other", []).append(d)
            elif et == "interaction.completed":
                usage = ev["interaction"].get("usage"); final_status = ev["interaction"].get("status")
            elif et and et.startswith("interaction."):
                final_status = ev.get("status") or ev.get("interaction", {}).get("status") or final_status
        model_steps, calls, text = [], [], ""
        for i in order:
            st = steps[i]
            if st.get("type") == "function_call":
                st["arguments"] = json.loads(st.pop("_args") or "{}"); calls.append(st)
            st.pop("_other", None)
            if st.get("type") == "model_output": text += "".join(c.get("text", "") for c in st.get("content", []))
            model_steps.append(st)
        log.append({"round": rnd + 1, "status": 200, "first_event_s": round(first, 2), "step_types": [s["type"] for s in model_steps],
                    "delta_kinds": sorted(set(kinds)), "signature_bytes": [len(s.get("signature", "")) for s in model_steps if s["type"] == "thought"],
                    "calls": [(c["name"], c["arguments"]) for c in calls], "final_status": final_status,
                    "usage": {k: usage.get(k) for k in ("total_input_tokens", "total_output_tokens", "total_thought_tokens", "total_tokens")} if usage else None,
                    "text": text[:160]})
        if not calls: break
        kept = [s for s in model_steps if not (drop_thoughts and s["type"] == "thought")]
        history = history + kept + [{"type": "function_result", "name": c["name"], "call_id": c["id"], "result": [{"type": "text", "text": run_tool(c["name"], c["arguments"])}]} for c in calls]
    return log

# ---- B: generateContent, streaming (the existing adapter's API) -------------
GTOOLS = [{"functionDeclarations": [{"name": t["name"], "description": t["description"], "parameters": t["parameters"]} for t in ITOOLS]}]
def generate(model, drop_signatures=False):
    contents = [{"role": "user", "parts": [{"text": QUESTION}]}]
    log = []
    for rnd in range(6):
        body = {"contents": contents, "tools": GTOOLS, "systemInstruction": {"parts": [{"text": SYSTEM}]}}
        status, events, headers, first = post(f"{BASE}/models/{model}:streamGenerateContent?alt=sse", body, True)
        if status != 200:
            log.append({"round": rnd + 1, "status": status, "error": events[:700]}); break
        parts, usage, finish = [], None, None
        for _, ev in events:
            for cand in ev.get("candidates", []):
                parts += cand.get("content", {}).get("parts", [])
                finish = cand.get("finishReason") or finish
            usage = ev.get("usageMetadata") or usage
        calls = [p for p in parts if "functionCall" in p]
        log.append({"round": rnd + 1, "status": 200, "first_event_s": round(first, 2), "part_kinds": [("functionCall" if "functionCall" in p else "text") + ("+sig" if "thoughtSignature" in p else "") for p in parts],
                    "calls": [(p["functionCall"]["name"], p["functionCall"].get("args")) for p in calls], "finish": finish,
                    "usage": {k: usage.get(k) for k in ("promptTokenCount", "candidatesTokenCount", "thoughtsTokenCount", "totalTokenCount")} if usage else None,
                    "text": "".join(p.get("text", "") for p in parts)[:160]})
        if not calls: break
        sent = [{k: v for k, v in p.items() if not (drop_signatures and k == "thoughtSignature")} for p in parts]
        contents = contents + [{"role": "model", "parts": sent}] + [{"role": "user", "parts": [{"functionResponse": {"name": p["functionCall"]["name"], "response": {"result": run_tool(p["functionCall"]["name"], p["functionCall"].get("args", {}))}}} for p in calls]}]
    return log

def show(title, log):
    print("==", title)
    for r in log: print("  ", json.dumps(r)[:900])

if len(sys.argv) != 3 or not KEY:
    sys.exit(__doc__)
mode, model = sys.argv[1], sys.argv[2]
if mode == "new":
    show(f"Interactions API, stateless, streamed, {model}", interactions(model))
elif mode == "new-drop":
    show(f"Interactions API, reasoning steps left out, {model}", interactions(model, drop_thoughts=True))
elif mode == "old":
    show(f"generateContent, streamed, {model}", generate(model))
elif mode == "old-drop":
    show(f"generateContent, signatures left out, {model}", generate(model, drop_signatures=True))
else:
    sys.exit(__doc__)
