package runs

import (
	"fmt"
	"strings"
)

// expectation is how every behavior the brief leaves open is settled
// (docs/specs/2026-10-04-run-decisions-design.md). In a live run a review
// chose the literal reading of a brief over what callers would expect, and the
// run failed on exactly that.
const expectation = "When the brief leaves a behavior open, choose what the people who use this would expect, not the narrowest reading of the brief's words."

// DecisionsHeading heads the plan's section of open behaviors and what was
// chosen for each. The runner copies the section into the pull request.
const DecisionsHeading = "## Decisions"

// rules go at the end of the session's first prompt. They hold for every
// later prompt too, which says so.
func rules() string {
	return `
## Rules for this run

- Do not push, and do not open a pull request. The runner does both.
- Nobody is watching, so there is no one to ask. If you are stuck on something, look it up first: web.search finds documentation and answers, and web.fetch reads a page. If you have tried that and still cannot settle it, ask Claude with claude.ask, saying what you tried and what you are choosing between.
- Commit your work as you go, and leave nothing uncommitted when you end your turn.
`
}

// maxTranscript is how much of the home's conversation a brief is written
// from, taken from the end.
const maxTranscript = 20000

// WorkPrompt is the session's first prompt: the whole run's work. transcript
// is the home's conversation, for a run given no brief, which the session
// writes first.
func WorkPrompt(r Run, transcript string) (prompt, goal string) {
	var brief string
	if r.Brief != "" {
		brief = fmt.Sprintf("The brief is in %s: read it first.", r.File(BriefFile))
	} else {
		if len(transcript) > maxTranscript {
			transcript = "(earlier conversation cut)\n\n" + transcript[len(transcript)-maxTranscript:]
		}
		brief = fmt.Sprintf(`There is no brief yet, so write it first, from the conversation at the end of this message, to %s. The brief says what is to be built or changed, and what done looks like, in the owner's own terms: what they asked for, what they ruled out, and anything they said about how it should behave. Keep it short, do not add requirements the owner did not state, and commit it.`, r.File(BriefFile))
	}
	prompt = fmt.Sprintf(`You are doing an automated run in this worktree, on the branch %[1]s, from %[2]s. It ends in a pull request, which the runner opens once Claude has reviewed your work.

%[3]s

Work in this order:

1. **Plan.** Write %[4]s: how the brief will be met and why that fits this codebase, the files to change, the order, the behaviors the tests must show, and the risks. Read the code it needs first, in proportion to the change. End it with a "%[5]s" section: one short paragraph for each behavior the brief leaves open that someone using the result could notice, saying what you chose, the alternative, and why. If the brief settles everything, say so in one line. The owner reads that section in the pull request. %[6]s Commit the plan.

2. **Tasks.** Write %[7]s as a checklist of the tasks that carry out the plan, each a coherent chunk of work that ends in a commit, with indented lines saying what it covers and the tests it writes:

   - [ ] Add the cache type
     A read-through cache in reader/cache.go with get and put. Tests: getReturnsWhatPutStored, getMissesAnUnknownKey.

   Commit it.

3. **Work.** Do the tasks in order. For each: write the code and its tests, build and run the tests, commit with a message saying what the task did, and tick its box in %[7]s ("- [x]") in the same commit.

4. **Finish.** When every task is ticked, run the project's full build and tests the way its CI does (read .github/workflows or the like, if there is one), fix what fails, commit, and end your turn.

If %[4]s and %[7]s already exist on the branch, an earlier session started this run: read them and the branch's log (`+"`git log origin/%[2]s..HEAD`"+`), and carry on from the first unticked task rather than starting again.

Do what the brief asks, and nothing more.
%[8]s%[9]s`, r.Branch, r.Base, brief, r.File(PlanFile), DecisionsHeading, expectation, r.File(TasksFile), rules(), conversation(r, transcript))
	goal = workGoal(r)
	return prompt, goal
}

// conversation is the home's conversation, for a run given no brief.
func conversation(r Run, transcript string) string {
	if r.Brief != "" {
		return ""
	}
	return "\n## The conversation\n\n" + transcript + "\n"
}

// workGoal is what the session's work is judged against.
func workGoal(r Run) string {
	return fmt.Sprintf("%s and %s are written, every task in %s is ticked, and the work is committed with the project's build and tests passing.",
		r.File(PlanFile), r.File(TasksFile), r.File(TasksFile))
}

// ContinuePrompt asks the session to carry on with the work after it ended
// its turn without finishing it, or after a failed try. missing says what is
// not done.
func ContinuePrompt(r Run, missing string) (prompt, goal string) {
	prompt = fmt.Sprintf(`The run is not done: %s

Carry on from where the work stands: %s, %s and `+"`git log origin/%s..HEAD`"+` say how far it got. Tick each task in %s as you finish it, commit as you go, and end your turn when every task is ticked and the build and tests pass.`,
		missing, r.File(PlanFile), r.File(TasksFile), r.Base, r.File(TasksFile))
	return prompt, workGoal(r)
}

// BlockersPrompt asks the session to do what the final review found. The
// runner has already added each blocker to tasks.md, unticked.
func BlockersPrompt(r Run, blockers []Blocker) (prompt, goal string) {
	var b strings.Builder
	for _, bl := range blockers {
		fmt.Fprintf(&b, "- **%s**: %s\n", strings.TrimSpace(bl.Title), strings.TrimSpace(bl.Detail))
	}
	prompt = fmt.Sprintf(`Claude reviewed the work against the brief, and found these blockers:

%s
The runner has added each one to %s as an unticked task. Do them the way you did the others: fix the code and its tests, build and test, commit, and tick the task. End your turn when every task is ticked and the build and tests pass.

%s The rules from the first message still hold.`, b.String(), r.File(TasksFile), expectation)
	return prompt, workGoal(r)
}

// NewSessionNote opens a prompt for a session the run made after its last one
// ended: it has none of the run in its context.
func NewSessionNote(r Run) string {
	return fmt.Sprintf(`You are taking over an automated run in this worktree, on the branch %s, from %s. An earlier session did its work: the brief is %s, the plan %s and the tasks %s, and `+"`git log origin/%s..HEAD`"+` shows what it committed. Read them before you start.
%s
`, r.Branch, r.Base, r.File(BriefFile), r.File(PlanFile), r.File(TasksFile), r.Base, rules())
}

// reviewer opens every prompt Claude gets.
const reviewer = "You are reviewing an automated coding run in this repository. You can read files; you cannot change them or run commands. The runner acts on your answer, so answer in exactly the form asked for.\n\n"

// FinalReviewPrompt asks Claude for blockers in the finished work, once.
func FinalReviewPrompt(r Run, changed []string) string {
	return reviewer + fmt.Sprintf(`The run's work is finished. Review it against the brief, %[1]s, before it becomes a pull request. The plan is %[2]s and the tasks %[3]s. The branch changed these files since %[4]s:

%[5]s

Report only blockers. A blocker is one of:
- the work does not do what the brief asks;
- it has a bug;
- what the brief asks for is not tested.

Style, naming, refactoring, suggestions, anything starting "consider", and anything the brief did not ask for are NOT blockers. Leave them out, or put them in notes, which go in the pull request description and change nothing. Every blocker becomes a task the run must do, so do not report one you would not insist on. Say what the code must do, and where a test would show it.

%[6]s The plan's "%[7]s" section records what the run chose for each behavior the brief left open. If you think one goes against what users would expect, say so in notes, not as a blocker: the decision is the owner's, made from the pull request.

Reply with one fenced json block:

`+"```json"+`
{"blockers": [{"title": "what must change", "detail": "where, and why it blocks"}], "notes": ["anything worth mentioning that is not a blocker"]}
`+"```"+`

For good work the right answer has no blockers.`, r.File(BriefFile), r.File(PlanFile), r.File(TasksFile), r.Base, "- "+strings.Join(changed, "\n- "), expectation, DecisionsHeading)
}

// maxOutput is how much of a CI failure a prompt carries, from the end, where
// a failure explains itself.
const maxOutput = 20000

func tail(s string) string {
	if len(s) > maxOutput {
		return "(earlier output cut)\n" + s[len(s)-maxOutput:]
	}
	return s
}

// CIFixPrompt asks the session to fix the PR's failing checks. failures is
// each failed check with the end of its log.
func CIFixPrompt(r Run, failures string) (prompt, goal string) {
	prompt = fmt.Sprintf(`The run's pull request is open, and its CI fails. These are the failed checks, with the end of each log:

%s

Find the cause and fix it, then commit; the runner pushes it. CI may check things your own run of the tests did not, such as formatting, another platform or a linter: fix what it reports, and run the build and tests again to be sure the fix breaks nothing else. If a failure is not the work's fault, such as a network error fetching a dependency, change nothing and say so. The rules from the first message still hold.`, strings.TrimSpace(tail(failures)))
	goal = "The CI failures listed in the last message are fixed and committed, or shown not to be the work's fault, and the project's build and tests pass."
	return prompt, goal
}
