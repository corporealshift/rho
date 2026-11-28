package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/corporealshift/rho/daemon/module"
	"github.com/corporealshift/rho/daemon/provider"
	"github.com/corporealshift/rho/daemon/session"
	"github.com/corporealshift/rho/daemon/tools"
	"github.com/corporealshift/rho/protocol"
)

// inPlace is a provider that summarizes in place, with a window large enough
// for a summary to fit after the request (docs/specs/2026-11-27-summary-on-the-cache-design.md).
func inPlace() provider.Config {
	off := false
	return provider.Config{Name: "fake", ContextWindow: 64000, SummarizeInPlace: true,
		KeepThinking: true, SummaryThinking: &off}
}

// newInPlaceHarness is newHarnessWithProvider with modules of its own.
func newInPlaceHarness(t *testing.T, pcfg provider.Config, mods []module.Module, script []provider.Response) *harness {
	t.Helper()
	dir := t.TempDir()
	store, err := session.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fake := &provider.Fake{Script: script}
	pr := provider.NewRegistry()
	pr.Add(pcfg, fake, true)
	builtins := &tools.Builtins{}
	mr := module.NewRegistry(append([]module.Module{builtins}, mods...), module.Options{Log: testLogger()})
	m, err := New(Deps{Store: store, Providers: pr, Modules: mr, Builtins: builtins,
		Root: dir, Log: testLogger()}, Config{DefaultModel: pcfg.Name + "/m"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	return &harness{m: m, fake: fake, store: store, dir: dir}
}

// primedWith runs one prompt to idle: a tool call, then a reply with thinking.
func primedWith(t *testing.T, h *harness, opts CreateOptions) *session.Session {
	t.Helper()
	s, err := h.m.Create(context.Background(), h.dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	h.m.Prompt(context.Background(), s.ID(), "go")
	h.m.WaitIdle(s.ID())
	return s
}

var primingTurns = []provider.Response{
	{ToolCalls: globCall("c1"), Reasoning: "look first"},
	{Content: "done", Reasoning: "that settles it"},
}

func compactionSummary(t *testing.T, ev protocol.Event) string {
	t.Helper()
	return protocol.MustData[protocol.CompactionData](ev).Summary
}

// The whole point: everything before the summary's own message is the request
// the server just read, so it is served from the cache, every result whole.
func TestASummaryInPlaceIsTheNextRequestPlusOne(t *testing.T) {
	h := newInPlaceHarness(t, inPlace(), nil, append(primingTurns, provider.Response{Content: "THE SUMMARY"}))
	s := primedWith(t, h, CreateOptions{})
	before := s.Events()

	ev, err := h.m.Compact(context.Background(), s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got := compactionSummary(t, ev); got != "THE SUMMARY" {
		t.Fatalf("summary: %q", got)
	}
	if n := h.fake.CallCount(); n != 3 {
		t.Fatalf("a summary in place is one call; the provider was called %d times", n)
	}

	sum := h.fake.Calls[2]
	last := sum.Messages[len(sum.Messages)-1]
	if last.Role != "user" || !strings.HasPrefix(last.Content, "Stop the work here") {
		t.Fatalf("the summary's own message must come last: %+v", last)
	}
	next := buildRequest(before, h.m.cfg.SystemPrompt, sum.Model, sum.Tools, 0, true)
	if !reflect.DeepEqual(sum.Messages[:len(sum.Messages)-1], next.Messages) {
		t.Fatalf("before its last message, a summary in place must be the next turn's request\n got %+v\nwant %+v",
			sum.Messages[:len(sum.Messages)-1], next.Messages)
	}

	// And so the last turn's request is a prefix of it: that is what the
	// server has cached.
	turn := h.fake.Calls[1]
	if !reflect.DeepEqual(sum.Messages[:len(turn.Messages)], turn.Messages) {
		t.Fatal("the last turn's request must be a prefix of the summary's")
	}
	if !reflect.DeepEqual(sum.Tools, turn.Tools) || len(sum.Tools) == 0 {
		t.Fatal("a summary in place must send the turn's tools: they are rendered ahead of the system message")
	}
	if !strings.Contains(last.Content, "What the files you read showed") {
		t.Fatal("a summariser that saw the files whole is asked to keep what they showed")
	}
	if sum.MaxTokens <= 0 || sum.MaxTokens > 64000 {
		t.Fatalf("the reply must be bounded by the window, got %d", sum.MaxTokens)
	}
}

// Strata's template renders the effort at the head of the system message, so
// turning thinking off for a summary would cost the whole cache.
func TestASummaryInPlaceSendsTheSessionsEffortNotNoThinking(t *testing.T) {
	h := newInPlaceHarness(t, inPlace(), nil, append(primingTurns, provider.Response{Content: "THE SUMMARY"}))
	s := primedWith(t, h, CreateOptions{Effort: protocol.EffortLow})
	if _, err := h.m.Compact(context.Background(), s.ID()); err != nil {
		t.Fatal(err)
	}
	turn, sum := h.fake.Calls[1], h.fake.Calls[2]
	if sum.NoThinking {
		t.Fatal("summary_thinking: false must not reach a summary in place")
	}
	if sum.Effort != protocol.EffortLow || sum.Effort != turn.Effort {
		t.Fatalf("effort: summary %q, turn %q, want both %q", sum.Effort, turn.Effort, protocol.EffortLow)
	}
}

// A session that never thinks sends no thinking to its summary either, so the
// summary's usual cap holds.
func TestASummaryInPlaceWithoutThinkingIsCapped(t *testing.T) {
	h := newInPlaceHarness(t, inPlace(), nil, append(primingTurns, provider.Response{Content: "THE SUMMARY"}))
	s := primedWith(t, h, CreateOptions{Effort: protocol.EffortNone})
	if _, err := h.m.Compact(context.Background(), s.ID()); err != nil {
		t.Fatal(err)
	}
	if got := h.fake.Calls[2].MaxTokens; got != summaryCap {
		t.Fatalf("cap: got %d, want %d", got, summaryCap)
	}
}

// Each way a summary in place can fail leaves the transcript summary to do the
// work, and says why.
func TestASummaryInPlaceFallsBackToTheTranscript(t *testing.T) {
	small := inPlace()
	small.ContextWindow = 8000 // a summary would not fit after the request

	cases := []struct {
		name   string
		pcfg   provider.Config
		script []provider.Response
		errs   map[int]error
		why    string
		calls  int
	}{
		{"it calls a tool", inPlace(),
			[]provider.Response{{ToolCalls: globCall("c9")}, {Content: "FROM THE TRANSCRIPT"}}, nil,
			"called a tool", 4},
		{"it writes nothing", inPlace(),
			[]provider.Response{{Content: "  "}, {Content: "FROM THE TRANSCRIPT"}}, nil,
			"wrote nothing", 4},
		{"the call fails", inPlace(),
			[]provider.Response{{}, {Content: "FROM THE TRANSCRIPT"}}, map[int]error{2: errors.New("server gone")},
			"server gone", 4},
		{"there is no room", small,
			[]provider.Response{{Content: "FROM THE TRANSCRIPT"}}, nil,
			"would be left for it", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newInPlaceHarness(t, tc.pcfg, nil, append(append([]provider.Response{}, primingTurns...), tc.script...))
			h.fake.Errors = tc.errs
			s := primedWith(t, h, CreateOptions{})
			ev, err := h.m.Compact(context.Background(), s.ID())
			if err != nil {
				t.Fatal(err)
			}
			if got := compactionSummary(t, ev); got != "FROM THE TRANSCRIPT" {
				t.Fatalf("summary: %q", got)
			}
			if n := h.fake.CallCount(); n != tc.calls {
				t.Fatalf("calls: got %d, want %d", n, tc.calls)
			}
			tr := h.fake.Calls[tc.calls-1]
			if user := tr.Messages[len(tr.Messages)-1].Content; !strings.HasPrefix(user, "<conversation>\n") {
				t.Fatalf("the fallback must be the transcript summary:\n%s", user)
			}
			var said bool
			for _, e := range s.Events() {
				if e.Type == protocol.EventNotice {
					msg := protocol.MustData[protocol.NoticeData](e).Message
					said = said || (strings.Contains(msg, "summarising in place did not work") && strings.Contains(msg, tc.why))
				}
			}
			if !said {
				t.Fatalf("a notice must say the summary in place did not work, and why (%q)", tc.why)
			}
		})
	}
}

// The system message must not change, so what a module asks the summary to
// keep goes in the summary's own message.
func TestPreservedFactsGoInTheLastMessage(t *testing.T) {
	h := newInPlaceHarness(t, inPlace(), []module.Module{&preserver{}},
		append(primingTurns, provider.Response{Content: "THE SUMMARY"}))
	s := primedWith(t, h, CreateOptions{})
	if _, err := h.m.Compact(context.Background(), s.ID()); err != nil {
		t.Fatal(err)
	}
	turn, sum := h.fake.Calls[1], h.fake.Calls[2]
	if sum.Messages[0].Content != turn.Messages[0].Content {
		t.Fatal("a preserved fact must not change the system message")
	}
	if last := sum.Messages[len(sum.Messages)-1].Content; !strings.Contains(last, "deploy key") {
		t.Fatalf("the preserved fact belongs in the summary's own message:\n%s", last)
	}
}

// An interrupt during a summary in place leaves the history alone, and does
// not go on to try a transcript.
func TestInterruptEndsASummaryInPlace(t *testing.T) {
	h := newInPlaceHarness(t, inPlace(), nil, primingTurns)
	s := primedWith(t, h, CreateOptions{})
	h.fake.BlockOn = make(chan struct{}) // never answers
	summariser := h.fake.CallCount()

	compacted := make(chan error, 1)
	go func() {
		_, err := h.m.Compact(context.Background(), s.ID())
		compacted <- err
	}()
	waitFor(t, "the summariser to be called", func() bool { return h.fake.CallCount() > summariser })
	if err := h.m.Interrupt(context.Background(), s.ID()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-compacted:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt did not end the summary")
	}
	if n := h.fake.CallCount(); n != summariser+1 {
		t.Fatalf("an interrupted summary in place must not fall back to a transcript: %d calls", n)
	}
	for _, e := range s.Events() {
		if e.Type == protocol.EventCompaction {
			t.Fatal("an interrupted summary appended a compaction")
		}
		if e.Type == protocol.EventNotice &&
			strings.Contains(protocol.MustData[protocol.NoticeData](e).Message, "did not work") {
			t.Fatal("an interrupt is not a failure to report")
		}
	}
}
