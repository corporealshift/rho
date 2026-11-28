# A summary is written on the session's own request

**Date:** 2026-11-27
**Status:** Approved by Kyle in conversation
**Amends:** the summarize call (`daemon/agent/compaction.go`) and provider config
(`docs/configuration.md`). Revisits the alternative "Show the summariser whole tool
results" that `2026-10-18-summary-keeps-reads-design.md` rejected.

## Problem

The tray session `01M8EQRC` on 2026-11-27 ran for eight hours and summarized ten times.
Each summary took 7 to 11 minutes, about an hour and a half in all, and after each one
the session read the same files again: `clients/goclient/client.go`, `cmd/rho/main.go`
and `cmd/rho/commands.go` were read whole seven times, once per window, and
`ARCHITECTURE.md` five. The first 40 events after every summary held 28,000 to 47,000
characters of tool output, most of it those reads.

Both have one cause. A summary is a new request: a system prompt of its own and a
transcript with every tool result cut to 1,200 bytes. None of it is in the server's
prompt cache, so Strata reads about 50,000 tokens from nothing at about 105 tokens a
second, which is most of the 7 to 11 minutes; writing a 3,000-token summary is under two.
And the summariser has seen 1,200 bytes of each file, so it cannot say what a file
showed. Since 2026-10-18 it says the file was read, which stops it calling the file
unread, but the agent that comes after has no more than that and reads it again.

The rejected alternative was to show the summariser whole results, because "every
summary would be a much longer prefill". It need not be any prefill at all. The request
the session sent for its last turn is already in the server's cache, every tool result
whole, and Strata has one slot.

## Decision

- **A provider setting, `summarize_in_place`.** When true, a summary is asked for on the
  session's own next request: the same system message, messages, tools and effort that
  its next turn would send, built by the same `buildRequest`, with one more user message
  at the end asking for the summary. Everything before that message is the request the
  server just saw, so it is read from the cache. Off by default: a server without a
  prefix cache gains nothing, and the transcript summary is unchanged.
- **The summary keeps what the files showed.** The instruction adds to the list it keeps:
  what the files read showed, where the next steps depend on it (names, signatures,
  types, line numbers, behaviour relied on), as facts and not pasted contents, so the
  agent after it need not read them again. This is only asked where the summariser has
  seen the files whole.
- **Nothing before the last message may differ.** In particular the summary sends the
  session's effort, and does not honour `summary_thinking: false`. Strata's Qwen3.8
  template renders `reasoning_instructions` at the very top of the system message from
  `enable_thinking` and `reasoning_effort`, so a summary without thinking would differ
  from the first tokens and be read entirely afresh. The instruction asks for short
  thinking instead. A session whose effort is `none` already sends no thinking, and its
  summary is capped as before.
- **It falls back to the transcript summary**, with a notice saying why, when the request
  would not leave room in the window for a summary, when the reply calls a tool, when it
  is empty, or when the call fails. The transcript summary is the one described in the
  earlier specs, unchanged.
- **The reply is bounded by the window.** Its cap is what the window has left after the
  request, so thinking and summary together cannot overflow it; with too little left
  (under twice the summary cap) the transcript summary is used.
- **Preserved facts go in the last message.** `BeforeCompaction` strings are appended to
  the instruction rather than the system message, which must not change.
- **No protocol change.** The compaction event, its range and what replaces it are the
  same; only how the summary text is obtained differs.

## Rejected

- **Turn thinking off for the summary anyway.** Measured on the template, not guessed:
  `enable_thinking: false` empties the reasoning instructions at the head of the system
  message, so the cache is lost from the first token and the summary is the full prefill
  it is today.
- **Prefill an empty thinking block as an assistant message.** It would skip thinking
  without touching the system message, but it depends on how a server renders a final
  assistant message, which differs between llama-server versions and is not verified on
  Strata. A follow-up if summaries prove to think at length.
- **Raise the 1,200-byte cut in the transcript summary.** Each byte is prefill there, so
  it trades minutes for what the cache gives free.
- **Make it the default.** A server without a prefix cache would read the whole session
  for every summary, longer than the transcript it reads today.

## Risks

- **Another request between the turn and the summary evicts the cache.** Strata has one
  slot, and a second session or a module call in between means the summary is read from
  nothing, about as slow as today but still correct, and it still sees every file whole.
- **Thinking at the session's effort makes the summary slower to write.** The instruction
  asks for short thinking. If summaries think for minutes, the prefill alternative above
  is the next step.
- **The summary sees thinking.** With `keep_thinking`, the request carries the session's
  thinking, which the transcript summary leaves out. The instruction asks for what was
  done and decided, as before.
- **The request is the session's largest.** It is the next turn's request, already under
  the summarize threshold by construction, plus about a thousand tokens of instruction.

## Proof

- `TestASummaryInPlaceIsTheNextRequestPlusOne`: with `summarize_in_place`, the summary
  request's messages are exactly the next turn's request's messages followed by one user
  message, with the same tools and effort, and the summary replaces the history.
- `TestASummaryInPlaceSendsTheSessionsEffortNotNoThinking`: `summary_thinking: false`
  does not reach a summary in place.
- `TestASummaryInPlaceFallsBackToTheTranscript`: a reply that calls a tool, an empty
  reply, a failed call, and a window with no room each produce the transcript summary
  and a notice.
- `TestPreservedFactsGoInTheLastMessage`.
- The existing transcript summary tests, unchanged, with the setting off.
- Live: with `summarize_in_place: true` on `strata`, a summary's usage shows most of its
  input as cached tokens, it is written in a few minutes rather than 7 to 11, and the
  window after it reads fewer files the session had already read.
