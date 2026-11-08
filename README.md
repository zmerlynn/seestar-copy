# seestar-copy

seestar-copy is now part of
[seestar-wormhole](https://github.com/zmerlynn/seestar-wormhole), from its
[0.7.0 release](https://github.com/zmerlynn/seestar-wormhole/releases/tag/v0.7.0).
This repository is archived.

- The desktop app's Copy, Browse and Free up space are tabs in the
  seestar-wormhole app. See
  [copying](https://github.com/zmerlynn/seestar-wormhole/blob/v0.7.0/docs/copying.md).
- The `sync`, `find`, `browse` and `delete` commands are in
  `seestar-wormhole-cli`. Stop any running `seestar-copy-cli` before you start
  them, and rename `--state-dir` to `--history-dir`; your copy history carries
  over. Patterns now use .gitignore syntax, so check that yours still select
  what you expect. See the
  [file command reference](https://github.com/zmerlynn/seestar-wormhole/blob/v0.7.0/docs/reference/files-cli.md).

The last release of seestar-copy on its own is
[0.4.0](https://github.com/zmerlynn/seestar-copy/releases/tag/v0.4.0).
