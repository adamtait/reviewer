# shell-hello

A reviewer plugin in 40 lines of POSIX shell. It exists as a conformance test for
the plugin protocol: if this stops working, the protocol has grown a requirement
it should not have.

Try it by hand — the conversation is readable:

```sh
printf '%s\n' \
  '{"type":"hello","protocol":1,"host":"manual"}' \
  '{"type":"describe"}' \
  '{"type":"analyze","analyzer":"shell-hello","request":{"root":"."}}' \
  '{"type":"bye"}' | ./plugin.sh
```

Register it with a destination repository by adding it to `.review/config.yaml`:

```yaml
plugins:
  - id: shell-hello
    command: ./path/to/plugin.sh
```

See `docs/plugin-protocol.md` for the frame reference.
