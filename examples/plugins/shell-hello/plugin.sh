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
      # One finding, anchored to a line the diff actually changed.
      #
      # This is the part worth copying. A finding whose file and line fall outside
      # the changed ranges is dropped by the core, silently and with no warning
      # (ADR-0007) — so a plugin that reports at a fixed location looks like it is
      # working, logs a finding, and puts nothing in front of a human. Every field
      # needed to avoid that is in the request: `changed` carries the files and
      # their line ranges.
      #
      # Extracted with sed rather than a JSON parser to keep the no-dependencies
      # claim honest: the first changed file, and the first line of its first
      # range. A real plugin reports where the problem is; the point here is only
      # that it reports somewhere a reader will see it.
      file=$(printf '%s' "$frame" | sed -n 's/.*"changed":\[{"path":"\([^"]*\)".*/\1/p')
      # Cut to the first file's object before matching, so a greedy match cannot
      # take a later file's ranges.
      line=$(printf '%s' "$frame" | sed -e 's/.*"changed":\[{//' -e 's/}.*//' \
        | sed -n 's/.*"ranges":\[\[\([0-9]*\),.*/\1/p')

      if [ -z "$file" ] || [ -z "$line" ]; then
        # Nothing changed that this plugin can point at. Reporting anyway would
        # mean reporting into a void, so it says it found nothing — which is true.
        log "shell-hello: no changed lines to anchor a finding to"
        say '{"type":"findings","analyzer":"shell-hello","findings":[]}'
      else
        say '{"type":"findings","analyzer":"shell-hello","findings":[{"fingerprint":"","ruleId":"example/shell-plugin-ran","lane":"deterministic","confidence":"low","severity":"info","file":"'"$file"'","line":'"$line"',"message":"the shell-hello example plugin ran, and anchored this to a changed line so you can see it"}]}'
      fi
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
