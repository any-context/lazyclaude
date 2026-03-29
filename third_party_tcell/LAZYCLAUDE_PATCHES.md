# tcell patches for lazyclaude

Forked from github.com/gdamore/tcell/v2 v2.13.5.
Non-essential files (demos, views, logos, docs, CI, tests) removed.

## Patches

### tscreen.go:1037 — read buffer 128 -> 4096

tcell reads PTY input in 128-byte chunks. For multi-byte UTF-8 text
(e.g. Japanese), a character can be split across two reads. The second
read blocks until new PTY data arrives, but during a bracketed paste
all data is already in the buffer. The split causes the input parser
to stall with `transform.ErrShortSrc`, deadlocking the paste.

Increasing to 4096 ensures a typical paste fits in a single read.
