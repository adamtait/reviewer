#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Trial tool: score the design's lookup rules against the TypeScript compiler.

  compare_with_compiler.py INDEX PROJECT_ROOT TRUTH.jsonl

TRUTH.jsonl comes from compiler_truth.cjs, run in the same project. For each
sampled identifier the tool compares the compiler's definition with the answer
the rules give from the index, under the rules as first stated and as revised.
It prints counts, and the positions it scores as wrong, which stay private.
"""

import collections
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import scip_lookup as sl  # noqa: E402

if len(sys.argv) != 4:
    sys.exit(__doc__)
index_path, root, truth = sys.argv[1:4]
meta, docs, _ = sl.read_index(index_path); src = sl.Sources(root); idx = sl.Index(docs, src); own = sl.own_packages(root)
rows = [json.loads(l) for l in open(truth)]
def inside(p): return not p.startswith("..") and "node_modules" not in p
for revised in (False, True):
    c = collections.Counter(); wrong = []
    for r in rows:
        lines = src.lines(r["path"])
        col16 = r["character"]  # TypeScript counts UTF-16 units too
        label, targets, detail = sl.resolve_position(idx, r["path"], r["line"], col16, own, revised)
        in_repo = [d for d in r["defs"] if inside(d["path"])]
        out_repo = [d for d in r["defs"] if not inside(d["path"])]
        if label == "definition": c["position is itself a definition"] += 1; continue
        if in_repo:
            want = {(d["path"], d["line"]) for d in in_repo}
            if label == "index":
                if any((t[0], t[1]) in want for t in targets): c["in repo: right"] += 1
                else: c["in repo: WRONG"] += 1; wrong.append((r["path"], r["line"] + 1, r["text"], targets[:2], sorted(want)[:2]))
            else: c[f"in repo: not resolved ({label}: {detail[:28]})"] += 1
        elif out_repo:
            if label == "index": c["outside repo: WRONG, resolved inside"] += 1; wrong.append((r["path"], r["line"] + 1, r["text"], targets[:2], "outside"))
            else: c[f"outside repo: {label}"] += 1
        else:
            c[f"compiler has no definition: {label}"] += 1
    print("revised rules" if revised else "rules as first stated", "- positions:", len(rows))
    for k, n in sorted(c.items()): print(f"  {n:5d}  {k}")
    for w in wrong[:6]: print("    wrong:", w)
