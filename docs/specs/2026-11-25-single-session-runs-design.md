# A run is one session

**Date:** 2026-11-25
**Status:** Approved by Kyle in conversation
**Supersedes:** the step-per-session workflow of 2026-09-30-orchestrated-runs, and with it
2026-10-09-verify-before-tasks, 2026-10-12-run-replan, 2026-10-17-plan-in-the-file,
2026-10-17-run-map and 2026-10-18-plan-outline.
**Amends:** `clients/runs` (state, transition, runner, prompts, config, status), the Run
page on the phone, and `/skip` in the terminal UI.

## Problem

The runner was built around a local model that drifted from the task, repeated itself
and said it was done when it was not. Each run went through about twenty fixed steps,
each its own short session with one file to produce: brief, plan, plan-review, verify,
verify-review, tasks, one session per task, check, fix, revise, replan-review,
final-review, pr, ci, ci-fix, reflect. Claude reviewed the plan, the check and the
finished work.

Kyle now runs a stronger local model, which can hold a whole run in one session. For it
the breakdown costs more than it saves: every session starts cold and reads the same code
again, every gate is a wait, and a run's context is spread over a dozen sessions.

## Decision

1. **One session does the run's work.** Its first prompt asks it to write the brief, if
   the run was given none, then the plan (`plan.md`, ending in the Decisions section the
   PR copies), then the tasks (`tasks.md`, a checklist), then to do the tasks in order,
   ticking each in `tasks.md` as it commits it, and to finish with the project's full
   build and tests.
2. **The same session gets the run's later prompts:** the final review's blockers, which
   the runner also adds to `tasks.md` unticked; each CI failure; and the reflection. It
   sits idle between them and holds no slot then. It is stopped when the run ends.
3. **Claude reviews the finished work only.** The plan review, the check and its review,
   revision and re-plan requests are gone. The final review now also counts untested
   behavior the brief asks for as a blocker, since there is no check to catch it.
4. **`verify.sh` is gone.** The session builds and tests as it goes (the daemon's stop
   gates already refuse a stop over failing tests), Claude reviews the diff, and CI is
   the last word.
5. **A session that ends its turn short** of the step (plan or tasks missing, tasks
   unticked, changes not committed) is told what is missing and carries on, up to three
   times per step, before that is a failed try. The runner commits the run's own files
   when the session forgets to. A failed try goes back to the same session, saying what
   is still missing. A session that ended any other way (stopped, out of turns, broken)
   is replaced: what it committed stays, what it left uncommitted goes, and the new
   session gets the whole work prompt, which says to carry on from the first unticked
   task. A new session at ci-fix is first told where the run stands.
6. **Kept as they were:** the PR and CI loop (`MaxCIFixes`), merging a goal's runs,
   reporting to the issue, the reflection, parking a gate while Claude is not answering,
   and `run:skip`, which now has only the final review to skip.
7. **Config:** `turns` caps the run's session over the whole run, and `effort` is one
   level for it. `plan_turns`, `work_turns`, `reflect_turns`, `verify_timeout` and the
   per-step effort map are gone.
8. **Runs saved before this** at a step that is gone pick up at work in a new session,
   or at CI if they have a PR. A failed one resumes the same way.

## Rejected

- **Keeping the plan review**, as a pause between plan and work in the same session.
  Kyle chose the final review alone.
- **Keeping `verify.sh`, written by the session.** A check the worker writes for itself
  proves little, and its fix loop was most of the runner's complexity.
