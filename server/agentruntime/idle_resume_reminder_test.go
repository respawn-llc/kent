package runtime

import (
	"context"
	"testing"
	"time"

	"core/server/llm"
	"core/shared/textutil"
)

func TestIdleResumeEligibility(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	threshold := 3600
	for _, test := range []struct {
		name string
		age  time.Duration
		want bool
	}{
		{"recent", time.Minute, false},
		{"equal", time.Hour, false},
		{"older", time.Hour + time.Nanosecond, true},
		{"backward", -time.Hour, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			last := now.Add(-test.age)
			if got := idleResumeEligible(&threshold, &last, now); got != test.want {
				t.Fatalf("eligible = %v, want %v", got, test.want)
			}
		})
	}
	if idleResumeEligible(&threshold, nil, now) || idleResumeEligible(nil, &now, now.Add(2*time.Hour)) {
		t.Fatal("absent activity or disabled policy qualified")
	}
}

func TestIdleReminderPrecedesAccompanyingHumanInput(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("first")}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("second")}},
	}}
	threshold := 3600
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", IdleResumeReminderSeconds: &threshold,
	})
	now := time.Now().Add(-3 * time.Hour)
	engine.transcriptRuntimeState().now = func() time.Time { return now }
	if _, err := engine.SubmitUserMessage(context.Background(), "first input"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := engine.SubmitUserMessage(context.Background(), "delayed input"); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("model requests = %d", len(client.calls))
	}
	messages := requestMessages(client.calls[1])
	if len(messages) < 2 {
		t.Fatalf("request messages = %v", messages)
	}
	reminder, human := messages[len(messages)-2], messages[len(messages)-1]
	if reminder.Role != llm.RoleDeveloper || reminder.MessageType == nil || *reminder.MessageType != llm.MessageTypeIdleResumeReminder ||
		human.Role != llm.RoleUser || human.Content == nil || *human.Content != "delayed input" {
		t.Fatalf("request tail = %+v, %+v", reminder, human)
	}
	entries := mustNewestTranscriptFacts(t, engine)
	found := false
	for index, entry := range entries {
		if entry.Notice != nil && entry.Notice.MessageType == llm.MessageTypeIdleResumeReminder {
			found = true
			if index+1 >= len(entries) || entries[index+1].User == nil {
				t.Fatal("persisted reminder does not immediately precede human input")
			}
		}
	}
	if !found {
		t.Fatal("persisted transcript has no typed reminder")
	}
}

func TestIdleResumeDuration(t *testing.T) {
	for _, test := range []struct {
		elapsed time.Duration
		want    string
	}{
		{59 * time.Second, "0m"},
		{24*time.Minute + 59*time.Second, "24m"},
		{time.Hour, "1h"},
		{(17*24+8)*time.Hour + 24*time.Minute + 59*time.Second, "17d8h24m"},
		{48 * time.Hour, "2d"},
	} {
		if got := formatIdleResumeDuration(test.elapsed); got != test.want {
			t.Fatalf("%v formatted as %q, want %q", test.elapsed, got, test.want)
		}
	}
}

func TestIdleReminderInputFreeContinuationEligibility(t *testing.T) {
	for _, test := range []struct {
		name         string
		conversation bool
		activity     bool
		disabled     bool
		age          time.Duration
		want         int
	}{
		{name: "resumed work", conversation: true, activity: true, age: 2 * time.Hour, want: 1},
		{name: "recent work", conversation: true, activity: true, age: time.Minute},
		{name: "no activity", conversation: true},
		{name: "disabled", conversation: true, activity: true, disabled: true, age: 2 * time.Hour},
		{name: "fresh session", activity: true, age: 2 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")}}}}
			threshold := 3600
			policy := &threshold
			if test.disabled {
				policy = nil
			}
			engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{Model: "gpt-6-sol", IdleResumeReminderSeconds: policy})
			if test.conversation {
				if err := engine.steerRuntime(steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true,
					[]llm.Message{{Role: llm.RoleAssistant, Content: textutil.Value("prior work")}})); err != nil {
					t.Fatal(err)
				}
			}
			if test.activity {
				if err := store.RecordModelToolActivity(time.Now().Add(-test.age)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := runStepLoopInActiveTestRun(t, t.Context(), engine); err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 {
				t.Fatalf("requests=%d", len(client.calls))
			}
			if got := idleReminderCount(client.calls[0]); got != test.want {
				t.Fatalf("reminders=%d, want %d", got, test.want)
			}
		})
	}
}

func idleReminderCount(request llm.Request) int {
	count := 0
	for _, message := range requestMessages(request) {
		if message.MessageType != nil && *message.MessageType == llm.MessageTypeIdleResumeReminder {
			count++
		}
	}
	return count
}

func TestFirstPostCompactionInputExcludesIdleReminder(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeCompactionClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("summary")}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("first answer")}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("later answer")}},
	}}
	threshold := 3600
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol", CompactionMode: "local", IdleResumeReminderSeconds: &threshold,
	})
	now := time.Now().Add(-6 * time.Hour)
	engine.transcriptRuntimeState().now = func() time.Time { return now }
	if err := store.RecordModelToolActivity(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := engine.steerRuntime(steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true,
		[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("prior input")}})); err != nil {
		t.Fatal(err)
	}
	engine.compactionRuntimeState().SetManualCompactionEligible(true)
	scheduleManualCompactionAndWait(t, engine)
	if store.ContextFacts().LastModelToolActivityAt == nil {
		t.Fatal("compaction cleared activity")
	}
	now = now.Add(2 * time.Hour)
	if _, err := engine.SubmitUserMessage(t.Context(), "first post-compaction"); err != nil {
		t.Fatal(err)
	}
	if got := idleReminderCount(client.calls[1]); got != 0 {
		t.Fatalf("first post-compaction reminders = %d", got)
	}
	now = now.Add(2 * time.Hour)
	if _, err := engine.SubmitUserMessage(t.Context(), "later post-compaction"); err != nil {
		t.Fatal(err)
	}
	if got := idleReminderCount(client.calls[2]); got != 1 {
		t.Fatalf("later post-compaction reminders = %d", got)
	}
}

func TestIdleReminderReusesUnansweredTailOnRetry(t *testing.T) {
	store := mustCreateTestSession(t)
	client := &fakeClient{
		errors: []error{&llm.ProviderAPIError{ProviderID: "openai", StatusCode: textutil.Value(401), Code: llm.UnifiedErrorCodeAuthentication}, nil},
		responses: []llm.Response{
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("retried")}},
			{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("later")}},
		},
	}
	threshold := 3600
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{Model: "gpt-6-sol", IdleResumeReminderSeconds: &threshold})
	now := time.Now().Add(-3 * time.Hour)
	engine.transcriptRuntimeState().now = func() time.Time { return now }
	if err := engine.steerRuntime(steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true,
		[]llm.Message{{Role: llm.RoleAssistant, Content: textutil.Value("prior work")}})); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordModelToolActivity(now.Add(-2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SubmitUserMessage(t.Context(), "resume"); err == nil {
		t.Fatal("expected provider failure")
	}
	if _, err := runStepLoopInActiveTestRun(t, t.Context(), engine); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 || idleReminderCount(client.calls[0]) != 1 || idleReminderCount(client.calls[1]) != 1 {
		t.Fatalf("ordinary retry duplicated or lost reminder: %+v", client.calls)
	}
	now = now.Add(2 * time.Hour)
	if _, err := engine.SubmitUserMessage(t.Context(), "later"); err != nil {
		t.Fatal(err)
	}
	if idleReminderCount(client.calls[2]) != 2 {
		t.Fatal("completed intervening model work suppressed a later idle reminder")
	}
}
