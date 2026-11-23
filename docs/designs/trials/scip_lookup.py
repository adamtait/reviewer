#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Trial tool for entry condition E2 of the review workspace design.

Reads an index.scip written by scip-typescript and applies the lookup rules in
docs/designs/review-workspace.md ("Navigation"), so the rules can be scored on a
real repository before any service exists. Standard library only.

  scip_lookup.py stats  INDEX PROJECT_ROOT
      Apply the rules to every reference in the index and print how many get
      each label, how many targets the name check discards, and examples.

  scip_lookup.py lookup INDEX PROJECT_ROOT PATH LINE COLUMN
      Answer one go-to-definition as the design would. LINE and COLUMN are
      1-based, as an editor shows them.

This is a throwaway measuring tool, not the service's decoder.
"""

import collections
import os
import re
import sys

DEFINITION = 0x1  # SymbolRole.Definition in the SCIP schema
UTF16 = 2  # PositionEncoding.UTF16CodeUnitOffsetFromLineStart


# --- protobuf wire format ---------------------------------------------------


def varint(buf, i):
    shift = value = 0
    while True:
        b = buf[i]
        i += 1
        value |= (b & 0x7F) << shift
        if not b & 0x80:
            return value, i
        shift += 7


def fields(buf):
    """Yield (field number, wire type, value) for one message."""
    i = 0
    while i < len(buf):
        key, i = varint(buf, i)
        number, wire = key >> 3, key & 7
        if wire == 0:
            value, i = varint(buf, i)
        elif wire == 2:
            size, i = varint(buf, i)
            value = buf[i : i + size]
            i += size
        elif wire == 1:
            value, i = buf[i : i + 8], i + 8
        elif wire == 5:
            value, i = buf[i : i + 4], i + 4
        else:
            raise ValueError(f"unsupported wire type {wire}")
        yield number, wire, value


def packed(buf):
    out, i = [], 0
    while i < len(buf):
        v, i = varint(buf, i)
        out.append(v)
    return out


# --- the parts of the schema this tool reads --------------------------------

Occurrence = collections.namedtuple("Occurrence", "path range symbol roles")


def read_range(raw_range, single, multi):
    """Return (start line, start char, end line, end char). Typed forms win."""
    if single is not None:
        f = {n: v for n, _, v in fields(single)}
        line = f.get(1, 0)
        return (line, f.get(2, 0), line, f.get(3, 0))
    if multi is not None:
        f = {n: v for n, _, v in fields(multi)}
        return (f.get(1, 0), f.get(2, 0), f.get(3, 0), f.get(4, 0))
    if len(raw_range) == 3:
        return (raw_range[0], raw_range[1], raw_range[0], raw_range[2])
    if len(raw_range) == 4:
        return tuple(raw_range)
    return None


def read_index(path):
    with open(path, "rb") as f:
        buf = f.read()
    meta = {"tool": "", "version": "", "project_root": ""}
    documents = []
    range_forms = collections.Counter()
    for number, _, value in fields(buf):
        if number == 1:
            for n, _, v in fields(value):
                if n == 2:
                    for tn, _, tv in fields(v):
                        if tn == 1:
                            meta["tool"] = tv.decode()
                        elif tn == 2:
                            meta["version"] = tv.decode()
                elif n == 3:
                    meta["project_root"] = v.decode()
        elif number == 2:
            doc = {"path": "", "encoding": 0, "occurrences": []}
            raw_occurrences = []
            for n, wire, v in fields(value):
                if n == 1:
                    doc["path"] = v.decode()
                elif n == 2:
                    raw_occurrences.append(v)
                elif n == 6:
                    doc["encoding"] = v
            for raw in raw_occurrences:
                rng, symbol, roles, single, multi = [], "", 0, None, None
                for n, wire, v in fields(raw):
                    if n == 1:
                        rng.extend(packed(v) if wire == 2 else [v])
                    elif n == 2:
                        symbol = v.decode()
                    elif n == 3:
                        roles = v
                    elif n == 8:
                        single = v
                    elif n == 9:
                        multi = v
                range_forms[
                    "typed" if single is not None or multi is not None else "list"
                ] += 1
                span = read_range(rng, single, multi)
                if span is not None and symbol:
                    doc["occurrences"].append(
                        Occurrence(doc["path"], span, symbol, roles)
                    )
            documents.append(doc)
    return meta, documents, range_forms


# --- symbol identifiers -------------------------------------------------------

NAME = r"(?:`(?:[^`]|``)+`|[A-Za-z0-9_+\-$]+)"
DESCRIPTOR = re.compile(
    rf"(?P<n1>{NAME})(?:/|#|:|!|\.|\((?:{NAME})?\)\.)"  # namespace, type, meta, macro, term, method
    rf"|\[(?P<n2>{NAME})\]"  # type parameter
    rf"|\((?P<n3>{NAME})\)"  # parameter
)


def split_symbol(symbol):
    """Return (is_local, package name, package version, last descriptor name)."""
    if symbol.startswith("local "):
        return True, "", "", ""
    # Scheme, manager, package name and version are separated by single spaces;
    # a space inside one of them is written as two spaces.
    parts, current, i = [], "", 0
    while i < len(symbol) and len(parts) < 4:
        ch = symbol[i]
        if ch == " ":
            if symbol[i + 1 : i + 2] == " ":
                current += " "
                i += 2
                continue
            parts.append(current)
            current = ""
            i += 1
            continue
        current += ch
        i += 1
    descriptors = symbol[i:] if len(parts) == 4 else ""
    last = ""
    for m in DESCRIPTOR.finditer(descriptors):
        last = m.group("n1") or m.group("n2") or m.group("n3") or last
    if last.startswith("`"):
        last = last[1:-1].replace("``", "`")
    name = parts[2] if len(parts) > 2 else ""
    version = parts[3] if len(parts) > 3 else ""
    return False, name, version, last


# --- source text -------------------------------------------------------------


class Sources:
    def __init__(self, root):
        self.root = root
        self.cache = {}

    def lines(self, path):
        if path not in self.cache:
            try:
                with open(os.path.join(self.root, path), encoding="utf-8") as f:
                    self.cache[path] = f.read().split("\n")
            except (OSError, UnicodeDecodeError):
                self.cache[path] = None
        return self.cache[path]

    def text(self, path, span):
        """Text at a single-line span, counting UTF-16 units as the index does."""
        lines = self.lines(path)
        line, start, end_line, end = span
        if lines is None or line != end_line or line >= len(lines):
            return None
        units = lines[line].encode("utf-16-le")
        if end * 2 > len(units):
            return None
        return units[start * 2 : end * 2].decode("utf-16-le", errors="replace")


# --- the design's rules ------------------------------------------------------


class Index:
    def __init__(self, documents, sources):
        self.sources = sources
        self.by_path = {}
        self.definitions = collections.defaultdict(list)
        self.dropped_encoding = 0
        self.dropped_missing = 0
        for doc in documents:
            if doc["encoding"] != UTF16:
                self.dropped_encoding += 1
                continue
            if sources.lines(doc["path"]) is None:
                self.dropped_missing += 1
                continue
            self.by_path[doc["path"]] = doc["occurrences"]
            for occ in doc["occurrences"]:
                if occ.roles & DEFINITION:
                    self.definitions[self.key(occ)].append(occ)

    @staticmethod
    def key(occ):
        # A local symbol is scoped to its document.
        if occ.symbol.startswith("local "):
            return (occ.path, occ.symbol)
        return ("", occ.symbol)

    def resolve(self, occ):
        """Return (label, passing targets, discarded targets, detail)."""
        local, package, version, last = split_symbol(occ.symbol)
        asked = self.sources.text(occ.path, occ.range)
        targets = self.definitions.get(self.key(occ), [])
        if targets:
            passing, discarded = [], []
            for t in targets:
                found = self.sources.text(t.path, t.range)
                ok = found is not None and (
                    found == asked or (not local and found == last)
                )
                (passing if ok else discarded).append((t, found))
            if passing:
                return "index", passing, discarded, ""
            return "text_search", [], discarded, "name check discarded every target"
        if not local and package not in ("", "."):
            return "external", [], [], f"{package} {version}"
        return "text_search", [], [], "no definition in the index"


def own_packages(root):
    """Names in every package.json under the project root, outside node_modules."""
    names = set()
    for directory, subdirs, files in os.walk(root):
        subdirs[:] = [d for d in subdirs if d not in ("node_modules", ".git")]
        if "package.json" in files:
            try:
                with open(os.path.join(directory, "package.json"), encoding="utf-8") as f:
                    m = re.search(r'"name"\s*:\s*"([^"]+)"', f.read())
                if m:
                    names.add(m.group(1))
            except OSError:
                pass
    return names


def stats(index_path, root):
    meta, documents, range_forms = read_index(index_path)
    sources = Sources(root)
    index = Index(documents, sources)
    own = own_packages(root)

    labels = collections.Counter()
    discard_examples = collections.Counter()
    external_own = external_other = 0
    references = 0
    ties = 0
    for path, occurrences in index.by_path.items():
        by_range = collections.defaultdict(set)
        for occ in occurrences:
            by_range[occ.range].add(occ.symbol)
        ties += sum(1 for symbols in by_range.values() if len(symbols) > 1)
        for occ in occurrences:
            if occ.roles & DEFINITION:
                continue
            references += 1
            label, passing, discarded, detail = index.resolve(occ)
            labels[label if not detail.startswith("name check") else "name_check_discard"] += 1
            if detail.startswith("name check"):
                asked = sources.text(occ.path, occ.range)
                for _, found in discarded:
                    discard_examples[(asked, found)] += 1
            if label == "external":
                if split_symbol(occ.symbol)[1] in own:
                    external_own += 1
                else:
                    external_other += 1

    def share(n):
        return f"{n:7d}  {100.0 * n / references:5.1f}%" if references else f"{n:7d}"

    print(f"tool                      {meta['tool']} {meta['version']}")
    # The project root is a path on the machine that built the index, so only
    # its presence is reported.
    print(f"project root recorded     {'yes' if meta['project_root'] else 'no'}")
    print(f"documents                 {len(documents)}")
    print(f"  dropped: not UTF-16     {index.dropped_encoding}")
    print(f"  dropped: file not found {index.dropped_missing}")
    print(f"range forms               {dict(range_forms)}")
    print(f"positions with two or more symbols on one range: {ties}")
    print(f"references looked up      {references}")
    print(f"  label 'index'           {share(labels['index'])}")
    print(f"  name check discards     {share(labels['name_check_discard'])}")
    print(f"  label 'external'        {share(labels['external'])}")
    print(f"    naming this repository's own package: {external_own}")
    print(f"    naming another package:               {external_other}")
    print(f"  no definition, no package (text search) {share(labels['text_search'])}")
    if discard_examples:
        print("most common name check discards (text asked about -> text at target):")
        for (asked, found), n in discard_examples.most_common(10):
            print(f"  {n:6d}  {asked!r} -> {found!r}")


def lookup(index_path, root, path, line, column):
    _, documents, _ = read_index(index_path)
    sources = Sources(root)
    index = Index(documents, sources)
    line0, col0 = line - 1, column - 1
    lines = sources.lines(path)
    if lines is None or line0 >= len(lines):
        sys.exit(f"cannot read {path}:{line}")
    # Columns are counted in UTF-16 units, as the index counts them.
    col16 = len(lines[line0][:col0].encode("utf-16-le")) // 2
    covering = [
        o
        for o in index.by_path.get(path, [])
        if o.range[0] == line0 == o.range[2] and o.range[1] <= col16 < o.range[3]
    ]
    if not covering:
        print("label: text_search (no occurrence in the index at this position)")
        return
    smallest = min(o.range[3] - o.range[1] for o in covering)
    tied = [o for o in covering if o.range[3] - o.range[1] == smallest]
    if len({o.symbol for o in tied}) > 1:
        print(f"note: {len({o.symbol for o in tied})} symbols share this range")
    for occ in tied:
        print(f"symbol: {occ.symbol}")
        if occ.roles & DEFINITION:
            print("  this position is itself a definition")
            continue
        label, passing, discarded, detail = index.resolve(occ)
        print(f"  label: {label}" + (f" ({detail})" if detail else ""))
        for t, _ in passing:
            print(f"  target: {t.path}:{t.range[0] + 1}:{t.range[1] + 1}")
        for t, found in discarded:
            print(f"  discarded by the name check: {t.path}:{t.range[0] + 1}:{t.range[1] + 1} holds {found!r}")


def main(argv):
    if len(argv) == 4 and argv[1] == "stats":
        stats(argv[2], argv[3])
    elif len(argv) == 7 and argv[1] == "lookup":
        lookup(argv[2], argv[3], argv[4], int(argv[5]), int(argv[6]))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv)
