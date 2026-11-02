# Iterations

Each file is a snapshot of `co` at one commit. To write the next one, start from the latest baseline below and read only what changed after it:

```
git log --oneline <baseline>..HEAD
git diff <baseline>..HEAD
```

Name the new file `<seq>-<features>.md`, using the next sequence number. Put that commit's full hash at the top, and add a row here.

| Seq | Snapshot | Baseline | Subject |
| --- | --- | --- | --- |
| 01 | [Read-only streaming TUI](01-readonly-streaming-tui.md) | `2db263a3da4186384ccfab80bb7e336b3aac46cf` | show streamed reasoning in the tui |
| 02 | [Providers, sessions, and a fullscreen transcript](02-providers-sessions-fullscreen.md) | `b745a2560c5627cab47f4723e4b9ed033b82fdd6` | generate session title |
