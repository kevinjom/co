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
| 03 | [Sessions, language tools, and themes](03-sessions-tools-themes.md) | `9a1897f9ed3643b656a1c7d6f87e29e824555ddf` | feat: @ to trigger fuzzy search files |
| 04 | [Writes and skills](04-writes-and-skills.md) | `f0999e064bbd2e500da8442ed41b7ff778f12394` | Merge pull request #4 from kevinjom/feat/skills |
