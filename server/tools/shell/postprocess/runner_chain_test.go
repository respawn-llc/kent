package postprocess

import (
	"context"
	"strings"
	"testing"
)

func TestChainSkipContinueAndHalt(t *testing.T) {
	calls := make([]string, 0, 4)
	envelope := NewEnvelope(Request{Output: "start"})
	chain := Chain{IDValue: "test", Processors: []Processor{
		testProcessor{id: "skip", fn: func(envelope Envelope) (Decision, error) {
			calls = append(calls, "skip")
			return Skip(envelope), nil
		}},
		testProcessor{id: "continue", fn: func(envelope Envelope) (Decision, error) {
			calls = append(calls, "continue")
			return Continue(envelope.WithCurrent(envelope.CurrentOutput+"-continue"), "continue"), nil
		}},
		testProcessor{id: "halt", fn: func(envelope Envelope) (Decision, error) {
			calls = append(calls, "halt")
			return Halt(envelope.WithCurrent(envelope.CurrentOutput+"-halt"), "halt"), nil
		}},
		testProcessor{id: "after-halt", fn: func(envelope Envelope) (Decision, error) {
			calls = append(calls, "after-halt")
			return Continue(envelope, "after-halt"), nil
		}},
	}}

	decision, err := chain.Process(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if decision.Action != ActionHalt {
		t.Fatalf("action = %q, want %q", decision.Action, ActionHalt)
	}
	if decision.Next.CurrentOutput != "start-continue-halt" {
		t.Fatalf("output = %q", decision.Next.CurrentOutput)
	}
	if strings.Join(calls, ",") != "skip,continue,halt" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestChainUnrecoverableProcessorErrorHaltsWithoutGoError(t *testing.T) {
	envelope := NewEnvelope(Request{Output: "start"})
	chain := Chain{IDValue: "test", Processors: []Processor{
		testProcessor{id: "unrecoverable", fn: func(Envelope) (Decision, error) {
			return Decision{}, ProcessorError{Severity: FailureUnrecoverable, Message: "cannot continue"}
		}},
		testProcessor{id: "after", fn: func(envelope Envelope) (Decision, error) {
			return Continue(envelope.WithCurrent("after"), "after"), nil
		}},
	}}

	decision, err := chain.Process(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if decision.Failure == nil || decision.Failure.Severity != FailureUnrecoverable {
		t.Fatalf("failure = %+v, want unrecoverable", decision.Failure)
	}
	if decision.Next.CurrentOutput != "start" {
		t.Fatalf("output = %q, want unchanged", decision.Next.CurrentOutput)
	}
}

func TestChainCriticalProcessorErrorPropagates(t *testing.T) {
	envelope := NewEnvelope(Request{Output: "start"})
	chain := Chain{IDValue: "test", Processors: []Processor{
		testProcessor{id: "critical", fn: func(Envelope) (Decision, error) {
			return Decision{}, ProcessorError{Severity: FailureCritical, Message: "stop now"}
		}},
	}}

	_, err := chain.Process(context.Background(), envelope)
	if err == nil {
		t.Fatal("expected critical processor error")
	}
	if !IsCriticalError(err) {
		t.Fatalf("err = %v, want critical processor error", err)
	}
}

func TestProxySkipsProcessorOutsideScope(t *testing.T) {
	called := false
	envelope := NewEnvelope(Request{CommandName: "npm", Output: "start"})
	processor := scopedTestProcessor{
		testProcessor: testProcessor{id: "scoped", fn: func(envelope Envelope) (Decision, error) {
			called = true
			return Continue(envelope.WithCurrent("called"), "scoped"), nil
		}},
		scope: Scope{CommandNames: []string{"go"}},
	}

	decision, err := Chain{IDValue: "test", Processors: []Processor{processor}}.Process(context.Background(), envelope)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if called {
		t.Fatal("processor should not run outside scope")
	}
	if decision.Action != ActionSkip || decision.Next.CurrentOutput != "start" {
		t.Fatalf("decision = %+v", decision)
	}
}

type testProcessor struct {
	id string
	fn func(Envelope) (Decision, error)
}

func (p testProcessor) ID() string { return p.id }

func (p testProcessor) Process(_ context.Context, envelope Envelope) (Decision, error) {
	return p.fn(envelope)
}

type scopedTestProcessor struct {
	testProcessor
	scope Scope
}

func (p scopedTestProcessor) Scope() Scope { return p.scope }
