# A summary keeps what was read

**Date:** 2026-10-18
**Status:** Approved by Kyle in conversation
**Amends:** the summary prompt and transcript (`daemon/agent/compaction.go`), and
2026-09-18-workspace-notes §6 (notes preserved through a summarize).

## Problem

Liftoff's plan session `01M56D3QR1Z9AKB4KF3Q8J8WXC` on 2026-10-17 took 112 minutes, with
four summaries. After each one it read again the files it had read before: the brief,
`LiftoffShell.kt`, `MissionScreen.kt`, `MissionState.kt`, `InFlightState.kt`. The
summaries told it to. The first one says:

- "`LiftoffShell.kt` (read, 9989 more bytes not fully examined)"
- "the brief truncated at 2549 more bytes; the rest of the brief has not been read"
- "Unread bodies: `SortieDao.updateNotes`…, `FlightPlanDao.getPlan`…"

The agent had read all of them whole. The summariser sees each tool result cut to 1,200
bytes, marked only with the bytes left out, and reported faithfully what it saw.

All four summaries also reached the 6,144-token cap and were cut off. About a third of
each was working notes from two other runs in the same repository, copied in verbatim,
because the notes module hands every note to the summariser as text it must preserve.
The notes module puts the same notes back in their own block on the next request anyway.

## Decision

1. **The summariser is told a cut read was seen whole.** A shortened tool result ends
   "…(shortened for this summary; the agent saw all N bytes)", and the summary prompt
   says a file the agent read has been read, never to list it as unread or not fully
   examined because its result is cut short, and to name a file the agent wrote its
   findings to rather than repeat them. Results are still cut at 1,200 bytes.
2. **Notes are not preserved through a summary.** `BeforeCompaction` in the notes module
   returns nothing, as the prompt and claude modules already do for blocks that go back
   word for word. The notes are on disk, and `BeforeRequest` sends them again after every
   summarize. The prompt's line that a later message from the user outranks what the
   agent wrote stays, now about anything the agent wrote.

## Rejected

- **Show the summariser whole tool results.** Every summary would be a much longer
  prefill; at about 105 tokens a second that is minutes per summary.
- **Raise the summary cap.** The cap was set because uncapped summaries ran to 16,000
  tokens and 10 minutes (2026-10-16-summary-length). The space was taken by copies.
- **Scope notes to a run's branch.** Other runs' notes cost about 2,500 tokens in every
  request. But notes are deliberately shared across worktrees (workspace-notes §3), and
  in the request they sit in a cached suffix block, so this is a smaller cost than the
  copies in the summary. A separate change if it earns one.

## Risks

- A summary may now contradict a note it no longer sees. The note is in the next request,
  newer than the summary, and the agent reads both.
- With a read marked as seen, the agent may act on detail the summary does not carry. It
  can read the file again; the difference is that the summary no longer tells it to.

## Proof

- `TestTheSummariserIsToldACutReadWasSeenWhole`: a long result is still cut, says the
  agent saw all of it, and the summary prompt says a cut read has been read.
- `TestCompactionIsToldToPreserveNothing` (notes), with the existing
  `TestNotesAreSentAgainAfterASummary`.
- Live: the next plan session's summaries do not list files as unread, are not cut off at
  the cap, and it reads fewer files twice than the 28 repeated calls of `01M56D3Q`.
