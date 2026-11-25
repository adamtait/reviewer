#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Trial tool: how many files would the workspace's secrets scan withhold?

Runs the pinned secrets scanner over one commit's files the way the design's
model gateway would (docs/designs/review-workspace.md, "How the scanner is
run"): every file is copied to <scratch>/<n>/item, so no file name, ignore file
or path exemption from the repository applies; the inline exemption comment is
disabled; decoding is on; and the scanner starts with no inherited environment.

It then runs the scanner once more over the tree as it is, with default
settings, which is close to what the CLI does today, so the two counts can be
compared.

  scan_pass.py TREE [GITLEAKS_BINARY]

TREE should hold tracked files only, for example:
  mkdir tree && git archive <commit> | tar -x -C tree

Prints counts and the paths that would be withheld. Prints no file content.
Standard library only.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile

FILE_BYTES = 1 << 20  # stand-in for the design's per-file limit


def text_files(tree):
    """Yield (relative path, bytes) for files the service would keep as text."""
    for directory, subdirs, files in os.walk(tree):
        subdirs[:] = sorted(d for d in subdirs if d != ".git")
        for name in sorted(files):
            full = os.path.join(directory, name)
            if os.path.islink(full) or os.path.getsize(full) > FILE_BYTES:
                continue
            with open(full, "rb") as f:
                data = f.read()
            if b"\0" in data:
                continue
            try:
                data.decode("utf-8")
            except UnicodeDecodeError:
                continue
            yield os.path.relpath(full, tree), data


def run(binary, target, extra, cwd):
    report = os.path.join(cwd, "report.json")
    command = [
        binary, "dir", "--no-banner", "--report-format", "json",
        "--report-path", report, "--exit-code", "1", *extra, target,
    ]
    done = subprocess.run(command, cwd=cwd, env={}, capture_output=True)
    if done.returncode not in (0, 1):
        sys.exit(f"the scanner failed: {done.stderr.decode(errors='replace')[:400]}")
    with open(report, encoding="utf-8") as f:
        return json.load(f) or []


def main(argv):
    if len(argv) not in (2, 3):
        sys.exit(__doc__)
    tree = os.path.abspath(argv[1])
    binary = shutil.which(argv[2] if len(argv) == 3 else "gitleaks")
    if not binary:
        sys.exit("gitleaks not found; pass its path as the second argument")
    version = subprocess.run([binary, "version"], capture_output=True, env={}).stdout.decode().strip()

    with tempfile.TemporaryDirectory() as work:
        scratch = os.path.join(work, "scan")
        os.mkdir(scratch)
        names = {}
        total = 0
        for n, (path, data) in enumerate(text_files(tree)):
            os.mkdir(os.path.join(scratch, str(n)))
            with open(os.path.join(scratch, str(n), "item"), "wb") as f:
                f.write(data)
            names[str(n)] = path
            total += len(data)

        planned = run(binary, scratch, ["--ignore-gitleaks-allow", "--max-decode-depth", "2"], work)
        today = run(binary, tree, [], work)

    def files_of(findings, base, rename=None):
        out = {}
        for finding in findings:
            rel = os.path.relpath(finding["File"], base)
            key = rename[rel.split(os.sep)[0]] if rename else rel
            out.setdefault(key, set()).add(finding["RuleID"])
        return out

    planned_files = files_of(planned, scratch, names)
    today_files = files_of(today, tree)

    print(f"scanner                          gitleaks {version}")
    print(f"text files scanned               {len(names)}")
    print(f"text bytes scanned               {total}")
    print(f"files withheld, planned settings {len(planned_files)}")
    print(f"files flagged, default settings  {len(today_files)}")
    print("files withheld under the planned settings (path: rules):")
    for path in sorted(planned_files):
        marker = "" if path in today_files else "   [not flagged by default settings]"
        print(f"  {path}: {', '.join(sorted(planned_files[path]))}{marker}")
    only_today = sorted(set(today_files) - set(planned_files))
    if only_today:
        # The default run reads a .gitleaks.toml at the root of TREE if there is
        # one, so it can apply rules of the repository's own.
        print("files flagged by default settings only (path: rules):")
        for path in only_today:
            print(f"  {path}: {', '.join(sorted(today_files[path]))}")


if __name__ == "__main__":
    main(sys.argv)
