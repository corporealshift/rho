# A plan is filled in from an outline

**Date:** 2026-10-18
**Status:** Approved by Kyle in conversation
**Amends:** the plan, tasks and plan-review prompts (`clients/runs/prompts.go`), and what the
runner does when a plan step begins (`clients/runs/runner.go`). Builds on
2026-10-17-plan-in-the-file and 2026-10-17-run-map, whose advice the model did not take.

## Problem

Liftoff's plan session `01M56D3QR1Z9AKB4KF3Q8J8WXC` took 112 minutes. Roughly:

| | |
|---|---|
| Reading tool results (668K characters, re-reads included) | ~40 min |
| Thinking (200K characters, 55% of its output) | ~24 min |
| Writing the plan and the map | ~7 min |
| Four summaries | 27 min |

Its first draft came 73 minutes in. The plan was 29 KB: Decisions 6.4K, Testing 4.2K, Screen
3.7K, Derived state 3.4K, Files 2.8K (mostly what the map says), with 3.2K of Kotlin. The
tasks session then wrote 23 KB of tasks from it, and every later session reads both.

The prompt said "Read as much of the repository as you need", set no size, and had no
outline, so the model read widely and designed every type before writing anything.

OpenSpec (Fission-AI/OpenSpec, `schemas/spec-driven`) plans differently:
- every document starts from a template of headings with a note under each;
- "Keep it concise (1-2 pages)";
- the design is "architecture and approach, not line-by-line implementation";
- "If a section would only restate them, point to them instead";
- "Write each file as soon as you have drafted it. Do not plan every file before writing
  the first";
- inspection is "proportional to the change".

## Decision

1. **The runner writes an outline.** When a plan step begins, before Start, the runner
   commits `plan.md` holding the outline: Approach, Files, Order, Testing, Risks and
   Decisions, each with an HTML comment saying what goes there and how much. Decisions stay
   last, where the pull request takes them from. After a re-plan the outline replaces the
   old plan, which stays in the branch's history. A retried step finds the outline already
   there and commits nothing.
2. **The plan session must change it.** A session that leaves the outline as it is is told
   once, in the same session, then thrown away, as a re-planned session that keeps the old
   plan already is.
3. **The prompt sets the order:**
   - Decisions first, from the brief alone, since they are behaviors a user would notice;
   - then reading, noting each file in the map;
   - then the other sections, each as soon as it is known, one edit per section;
   - then one read-through and the commit.
4. **Read in proportion.** "As much as you need" goes. The prompt says to read only what
   the plan needs, and of a long file only the part needed (grep -n, then an offset and a
   limit), because the work sessions read the code they change.
5. **Size.** About two pages, with no code, signatures or class designs; point to the brief
   or the map rather than repeat them. The tasks prompt keeps each entry to a few lines
   with no code. The plan review keeps the plan's sections and size.

## Rejected

- **More planning sessions, as OpenSpec has more documents.** Each session starts with a
  cold read of its prompt, minutes on the local model. The run already splits plan, verify
  and tasks; verify plays the part of OpenSpec's specs.
- **The outline only in the prompt.** The model ignored "write a first draft as soon as
  you can" and "add to the map as you read". A file that already exists with headings in
  it makes an edit the natural first move.
- **Lower effort for the plan.** It is already low. Most of the thinking went into designs
  the plan no longer asks for.
- **A size limit the runner enforces.** A plan that runs long is still a plan, and failing
  a session over length would cost a whole step.

## Risks

- A plan without signatures leaves more for the work sessions to find. The map is where
  those facts go.
- The model may fill the outline and leave a note comment in place. It renders as nothing.

## Proof

- `TestAPlanSessionFillsInTheOutline`:
  - the outline is in place and committed when the session starts;
  - a session that changes only the map is told once, then thrown away;
  - the retry does not commit the outline again;
  - a filled-in plan ends the step.
- `TestThePlanOutline`: every section, Decisions last.
- `TestSessionPrompts`: the plan prompt names the outline, the order, reading in
  proportion, the size, and no code; the tasks prompt, the size of an entry.
- Live: the next plan session's first section is written within minutes, and its plan
  is near 10 KB rather than 29 KB.
