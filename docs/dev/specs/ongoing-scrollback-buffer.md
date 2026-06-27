# Ongoing Scrollback Buffer

## Scope

This spec owns ongoing mode's normal-buffer terminal surface. Alt-screen surfaces, alternate-scroll mode changes, BEL, and OSC notifications are outside the stable/live surface contract.

The runtime transcript, event log, session view, and committed transcript read models remain authoritative. The client must not keep a physical-terminal ledger, physical projection, acknowledgement cursor, replay cursor, or emitted-byte copy of content already written into the normal-buffer stable zone.

## Zones

The ongoing normal-buffer surface has two zones:

- Stable zone: immutable terminal scrollback content.
- Live area: the visible mutable terminal area rendered from current UI state.

Stable-zone writes are fire-and-forget. After bytes are written to the stable zone, the client treats those bytes as unavailable terminal state. The client does not track what has been physically emitted there, and it does not rewrite, restyle, replay, gate, acknowledge, or synchronize emitted stable-zone content.

The live area is erased completely and re-rendered from current live-area state whenever the live area changes. It does not render on a clock. Animations produce changes, and those changes may schedule renders. When no live-area state changes or animation ticks exist, the live area stays stable and stops rendering.

## Ownership

Stable-zone byte writes have one stable owner. Live-area byte writes have one live owner. No other ongoing normal-buffer path writes terminal bytes directly.

The stable owner accepts only three stable write intents:

- Append one already-rendered stable line.
- Append an assistant markdown stream chunk.
- Finish the active assistant stream.

The live owner accepts complete live frames: pre-split rendered lines plus optional native cursor placement. It owns native cursor placement and restoration for ongoing mode. Ongoing mode does not use a separate normal-buffer cursor writer.

A stable surface has at most one live area attached to it. Stable and live ownership share one terminal writer, one geometry snapshot, and one mutual exclusion boundary.

## Normal-Buffer Ownership

Stable/live bytes are emitted only while the ongoing transcript surface owns the terminal normal buffer. Alt-screen and other non-ongoing surfaces never receive stable/live bytes.

When normal-buffer ownership is unavailable, stable write intents are buffered in order, including stable-line appends, assistant stream chunks, and assistant-stream finish. Live rendering keeps only the latest desired live frame. When normal-buffer ownership resumes, the stable owner flushes held stable intents and restores the latest live frame.

Ordinary alt-screen transitions do not recreate stable/live objects and do not rehydrate transcript history. Terminal resize recreates stable/live objects for the new geometry immediately, holds native normal-buffer writes while resize events are settling, then rehydrates stable scrollback from authoritative transcript state after one second without further resize events. Resize must not replay from physical-terminal emitted bytes, raw terminal caches, memoized render text, or alternate native-scrollback state.

## Write Ordering

Any stable-zone write request first requires full live-area erasure. The stable-zone write happens only after the current live-area contents are removed from the terminal. After the stable-zone write completes, the latest live area is restored before the terminal frame is considered complete.

Stable and live writes are emitted as one terminal frame under the shared exclusion boundary. A stable write with attached live content erases the live area, writes the stable bytes, restores the live area, and only then releases the boundary.

Erase failure skips both the stable write and live restore. Stable write failure still attempts live restore. Finishing assistant streaming erases once, flushes all queued stable-line appends, and restores once. If a held flush contains multiple stable writes, later writes are attempted after earlier failures.

Goroutines never write terminal bytes directly. A goroutine may only schedule work that later executes through approved terminal-main-thread write paths.

All terminal buffer writes assert terminal-main-thread ownership. Blocking or non-terminal work on that thread is a fatal programming error. File I/O, network I/O, subprocess waits, sleeps, and expensive CPU work on the terminal main thread fail unconditionally.

## Assistant Streaming

Only assistant deltas with structured commentary or final-answer phase may use native assistant stable streaming. Missing-phase deltas do not use native streaming; that assistant's committed transcript message is written as stable committed transcript content instead of finalizing a partial native stream.

Deltas received while normal-buffer ownership is unavailable keep their phase and remain buffered assistant stream chunks.

## Errors And Invariants

Immediate normal-buffer terminal write failures surface synchronously to the caller. Delayed holdoff flush failures surface through the native surface's delayed-error reporting path.

Contract and invariant violations fail fast with diagnostic detail. Diagnostics include the attempted operation, terminal geometry, calculated visual width when relevant, quoted payload or frame content, raw payload bytes when relevant, and stack trace.

The stable-line append intent accepts exactly one visual terminal line. Visual width is ANSI-aware display cell width according to the active terminal width. If the input occupies more than one terminal line or contains embedded carriage return or line feed, it is an invariant violation.

Live-area content must be non-empty, contain no embedded carriage return or line feed inside a submitted line, fit within terminal height, and have every line fit within terminal-width ANSI-aware display cells. Native cursor row and column must fit inside the submitted live frame.
