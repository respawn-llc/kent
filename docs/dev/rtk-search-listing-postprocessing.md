# RTK Search/Listing Post-Processing Inventory

RTK source inspected at `/Users/nek/Developer/Tools/rtk`, commit `55f998d08cd80ece970fe5e61eaae3533512288b`.

This tracked note mirrors key conclusions from ignored scratch audit `docs/tmp/rtk-post-processing-audit.md`, section `System / Meta Command Families` -> `2. Search/listing family`.

## Commands

- `grep`: proxies through `rg` first, falls back to `grep`; parses `file:line:content`; trims and match-biased truncates lines; groups by file; caps total and per-file rows; passes through count/list/only-match/null output flags when those flags land in `extra_args`.
- `find`: does an internal ignore-aware walk, not native `find`; supports limited native-like args and RTK args; rejects compound predicates/actions; groups basenames by parent dir; caps displayed files; prints extension summary.
- `ls`: always runs `ls -la` with `LC_ALL=C`; parses English-date `ls` output; drops permissions/owner/group/date; hides noise dirs unless all entries requested; emits dirs first, then files with human sizes; TTY adds file/dir/extension summary.
- `tree`: proxies native `tree`; injects `-I` with noise dirs unless all entries or ignore already requested; removes summary line and trims edge blank lines.
- `wc`: strips padding and paths; formats single-file counts as bare numbers or `<lines>L <words>W <bytes>B`; formats multi-file rows with common path prefix stripped and `Σ` total rows.
- `diff`: two-file mode performs same-index line comparison with char-set similarity for modified-vs-add/remove; stdin mode strips unified diff metadata, groups by file, prints all changed lines, but still appends stale `... +N more` when changes exceed 10.

Source anchors live in the scratch audit next to each command bullet.
