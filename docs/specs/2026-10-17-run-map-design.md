# A run keeps a map of the code

**Date:** 2026-10-17
**Status:** Approved by Kyle in conversation
**Amends:** the brief, plan, tasks, verify, work and fix prompts (`clients/runs/prompts.go`),
and which files a planning step may change (`clients/runs/runner.go`).

## Problem

With thinking sent back and the cache reused, Liftoff's plan session `01M55RAB` on
2026-10-17 still took 91 minutes. Of its 130 tool calls only 74 were distinct, and most of
the repeats came straight after a compaction:

- 20:22–20:40: it read about 25 files (the shell, MainActivity, the DAO, the in-flight
  screen and state, DESIGN.md, ARCHITECTURE.md, their tests). Compaction at 20:40, with
  no word of the plan written.
- 20:41–21:01: it read the brief again and the same files again, and wrote the first
  draft at 21:01. Compaction at 21:07.
- 21:07–21:29: it read them again, and rewrote the plan whole.

About half the session was reading what it had already read. What it learned was only in
the conversation, and a summary keeps the gist, not the paths, signatures and line
numbers a plan is made of. The instruction to draft in the file (2026-10-17-plan-in-the-file)
did not take: it read for 18 minutes first.

## Decision

A run has one more file, `map.md`: its notes on the code. The files involved, a line on
what each holds and why it matters, the types, functions and tests that already exist,
and the project docs that set rules. It is not the plan, and it is not the brief.

- **The brief session** starts it, after the brief, by looking up the code the brief
  touches. The brief itself stays in the owner's terms.
- **Plan, verify and tasks** start from it, read only what they still need, add to it as
  they read, and correct what turns out to be wrong. Where it does not exist yet, which is
  every run handed a brief (a goal's runs, /run with a brief), the first of them starts it.
- **Work and fix** sessions are told it is there, beside the brief and the plan.

A planning step may change its own file and the map, and nothing else. The runner commits
a map left uncommitted, as it does the step's own file. A session that changes only the
map has not written its file, and is told so as before.

The draft-in-the-file sentence now says why in terms of summaries, since thinking is sent
back: a summary keeps only what is in a file.

## Rejected

- **A separate map step.** One more session start and cold prompt, for reading the brief
  session or the plan session does anyway.
- **The map in brief.md.** Every session reads the brief as what is wanted; file lists
  would muddy it, and the pull request quotes it.
- **Only rewording the draft-in-the-file sentence.** It was already there and was not
  followed. A file whose only job is notes, there before the plan is, is an easier habit.
- **A map in each goal brief, written by Claude.** Claude reads the repository to break a
  goal down, so its map would be good and cost the local model nothing. But the brief
  reaches the run as the home's description, and the map would need a way of its own
  there. A follow-up, if the map earns it.

## Risks

- The brief session takes longer: it now reads code.
- A wrong map points a step the wrong way. Each step is told to correct it.

## Proof

- The brief, plan, tasks and verify prompts name the map and say to add to it; work and
  fix name it as context.
- A planning step that changes its file and the map keeps both, committed; one that changes
  any other file is still thrown away.
- Live: the next plan session reads far fewer files twice than the 56 repeats above, and
  writes plan.md before its first compaction.
