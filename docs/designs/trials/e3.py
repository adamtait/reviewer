#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Trial tool for entry condition E3 of the review workspace design.

Makes, with one fine-grained personal access token, the GitHub calls the
workspace service would make, and measures what the design needs to know:
whether each call works, how long one commit's archive takes to fetch and
unpack, how large it is in memory, and how long a text search over it takes.

  GITHUB_TOKEN=... e3.py --repo OWNER/NAME --head COMMIT --base BRANCH
        [--api URL] [--workflow review-index.yml] [--artifact review-index]
        [--search TEXT]

  e3.py --archive-file FILE.tar.gz [--search TEXT]
        Measure unpacking and search only, on an archive already on disk.

The token needs read access to Contents, Pull requests, Metadata and Actions.
It is read from the environment and never printed. The output names no
repository, so it can be pasted into a public document. Standard library only.
"""

import argparse
import io
import json
import os
import resource
import sys
import tarfile
import time
import urllib.error
import urllib.request
import zipfile

FILE_BYTES = 1 << 20  # stand-in for the design's per-file limit
AGENT = "review-workspace-trial"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


OPENER = urllib.request.build_opener(NoRedirect)


def fetch(url, token, accept="application/vnd.github+json"):
    """GET a GitHub API address. Returns (status, bytes, was redirected).

    A redirect is followed by hand and WITHOUT the token: archive and artifact
    downloads redirect to a signed address on another host, and sending the
    Authorization header there is both unnecessary and, for artifacts, refused.
    """
    request = urllib.request.Request(url, headers={
        "Authorization": f"Bearer {token}",
        "Accept": accept,
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": AGENT,
    })
    try:
        with OPENER.open(request, timeout=600) as response:
            return response.status, response.read(), False
    except urllib.error.HTTPError as error:
        if error.code in (301, 302, 303, 307, 308):
            location = error.headers.get("Location", "")
            follow = urllib.request.Request(location, headers={"User-Agent": AGENT})
            with urllib.request.urlopen(follow, timeout=600) as response:
                return response.status, response.read(), True
        return error.code, error.read(), False


def peak_memory_mb():
    peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    return peak / (1 << 20) if sys.platform == "darwin" else peak / 1024


def unpack(data):
    """Hold an archive in memory the way the design describes. Returns a report."""
    started = time.monotonic()
    kept = {}
    counts = {"entries": 0, "text": 0, "binary": 0, "too_large": 0, "other": 0}
    tops = set()
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:*") as archive:
        for member in archive:
            if member.name == "pax_global_header":
                continue
            counts["entries"] += 1
            tops.add(member.name.split("/", 1)[0])
            if not member.isfile():
                if not member.isdir():
                    counts["other"] += 1
                continue
            if member.size > FILE_BYTES:
                counts["too_large"] += 1
                continue
            content = archive.extractfile(member).read()
            try:
                if b"\0" in content:
                    raise UnicodeDecodeError("utf-8", b"", 0, 1, "NUL")
                kept[member.name] = content.decode("utf-8")
                counts["text"] += 1
            except UnicodeDecodeError:
                counts["binary"] += 1
    return {
        "kept": kept,
        "counts": counts,
        "kept_bytes": sum(len(t.encode("utf-8")) for t in kept.values()),
        "one_top_level_folder": len(tops) == 1,
        "seconds": time.monotonic() - started,
    }


def search(kept, needle):
    started = time.monotonic()
    low = needle.lower()
    matches = 0
    for text in kept.values():
        if low in text.lower():
            matches += sum(1 for line in text.split("\n") if low in line.lower())
    return matches, time.monotonic() - started


def report_archive(label, data, download_seconds, needle):
    result = unpack(data)
    c = result["counts"]
    print(f"{label}")
    if download_seconds is not None:
        print(f"  fetch seconds                {download_seconds:.1f}")
    print(f"  compressed bytes             {len(data)}")
    print(f"  unpack seconds               {result['seconds']:.1f}")
    print(f"  entries                      {c['entries']}")
    print(f"  text files kept              {c['text']}")
    print(f"  binary files                 {c['binary']}")
    print(f"  files over the size limit    {c['too_large']}")
    print(f"  links and other entries      {c['other']}")
    print(f"  kept text bytes              {result['kept_bytes']}")
    print(f"  one top-level folder         {'yes' if result['one_top_level_folder'] else 'no'}")
    if needle:
        matches, seconds = search(result["kept"], needle)
        print(f"  search for {needle!r}: {matches} matching lines in {seconds:.2f} seconds")
    return (download_seconds or 0) + result["seconds"]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--api", default=os.environ.get("GITHUB_API_URL", "https://api.github.com"))
    parser.add_argument("--repo")
    parser.add_argument("--head")
    parser.add_argument("--base")
    parser.add_argument("--workflow", default="review-index.yml")
    parser.add_argument("--artifact", default="review-index")
    parser.add_argument("--search", default="")
    parser.add_argument("--archive-file")
    args = parser.parse_args()

    if args.archive_file:
        with open(args.archive_file, "rb") as f:
            report_archive("archive from disk", f.read(), None, args.search)
        print(f"peak memory of this process, MB  {peak_memory_mb():.0f}")
        return

    token = os.environ.get("GITHUB_TOKEN", "")
    if not (token and args.repo and args.head and args.base):
        sys.exit(__doc__)
    if not token.startswith("github_pat_"):
        print("note: the token is not a fine-grained token; the design accepts only those")
    root = f"{args.api.rstrip('/')}/repos/{args.repo}"

    status, body, _ = fetch(root, token)
    print(f"1. read the repository           HTTP {status}")
    if status == 200:
        print(f"   private                       {'yes' if json.loads(body).get('private') else 'NO'}")

    status, body, _ = fetch(f"{root}/compare/{args.base}...{args.head}", token)
    merge_base = json.loads(body).get("merge_base_commit", {}).get("sha", "") if status == 200 else ""
    print(f"2. compare, merge base returned  HTTP {status}, {'yes' if merge_base else 'no'}")

    total = 0.0
    for label, sha in (("3. archive of the head commit, by full hash", args.head),
                       ("4. archive of the merge base", merge_base)):
        if not sha:
            print(f"{label}: skipped")
            continue
        started = time.monotonic()
        status, data, redirected = fetch(f"{root}/tarball/{sha}", token)
        seconds = time.monotonic() - started
        if status != 200:
            print(f"{label}: HTTP {status}")
            continue
        print(f"   answered through a redirect   {'yes' if redirected else 'no'}")
        total += report_archive(label, data, seconds, args.search)
    print(f"   both archives, one after the other, seconds  {total:.1f}")

    status, body, _ = fetch(
        f"{root}/actions/workflows/{args.workflow}/runs?head_sha={args.head}&event=push&per_page=100", token)
    runs = json.loads(body).get("workflow_runs", []) if status == 200 else []
    print(f"5. index workflow runs for the head commit  HTTP {status}, {len(runs)} run(s)")
    for run in runs:
        print(f"   run: status {run.get('status')}, conclusion {run.get('conclusion')}")
    succeeded = [r for r in runs if r.get("conclusion") == "success"]
    if succeeded:
        run = max(succeeded, key=lambda r: r.get("created_at", ""))
        status, body, _ = fetch(f"{root}/actions/runs/{run['id']}/artifacts?name={args.artifact}", token)
        artifacts = json.loads(body).get("artifacts", []) if status == 200 else []
        print(f"6. artifacts named {args.artifact!r} on the newest successful run  HTTP {status}, {len(artifacts)}")
        for artifact in artifacts:
            print(f"   size bytes {artifact.get('size_in_bytes')}, expired {artifact.get('expired')}")
            started = time.monotonic()
            status, data, redirected = fetch(f"{root}/actions/artifacts/{artifact['id']}/zip", token)
            seconds = time.monotonic() - started
            print(f"7. artifact download             HTTP {status}, redirect {'yes' if redirected else 'no'}, {seconds:.1f} seconds")
            if status == 200:
                with zipfile.ZipFile(io.BytesIO(data)) as z:
                    for info in z.infolist():
                        print(f"   entry {info.filename}: {info.file_size} bytes")
    else:
        print("6. no successful run, so no artifact to read")
    print(f"peak memory of this process, MB  {peak_memory_mb():.0f}")


if __name__ == "__main__":
    main()
