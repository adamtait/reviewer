#!/bin/sh
# SPDX-License-Identifier: MIT
#
# The smallest possible reviewer plugin, in POSIX shell with no dependencies.
# Its only job is to prove that the plugin protocol does not require a real
# language, an SDK, or a JSON library (ADR-0025).
#
# Protocol frames go to stdout, one JSON object per line. Anything a plugin wants
# to say to a human goes to stderr: a stray echo on stdout corrupts the stream.

set -eu

say() { printf '%s\n' "$1"; }
log() { printf '%s\n' "$1" >&2; }

log "shell-hello: started"

while IFS= read -r frame; do
  case "$frame" in
    *'"type":"hello"'*)
      # A real plugin would check the host's protocol number and refuse a
      # mismatch. This one only speaks version 1, and says so.
      say '{"type":"hello","protocol":1,"plugin":"shell-hello","version":"0.1.0"}'
      ;;

    *'"type":"describe"'*)
      # order 900 puts this after every real analyzer. lane is declared here
      # rather than inferred, which is what lets the core block the model lane
      # without trusting the plugin.
      say '{"type":"describe","analyzers":[{"id":"shell-hello","lane":"deterministic","order":900,"available":true}]}'
      ;;

    *'"type":"analyze"'*)
      # One fixed finding. It is reported against README.md:1, so in a real run
      # the core's diff filter drops it unless that file changed — which is a
      # demonstration of diff scoping rather than a bug.
      say '{"type":"findings","analyzer":"shell-hello","findings":[{"fingerprint":"","ruleId":"example/shell-plugin-ran","lane":"deterministic","confidence":"low","severity":"info","file":"README.md","line":1,"message":"the shell-hello example plugin ran"}]}'
      ;;

    *'"type":"bye"'*)
      log "shell-hello: exiting"
      exit 0
      ;;

    *)
      log "shell-hello: ignoring unrecognised frame"
      ;;
  esac
done
