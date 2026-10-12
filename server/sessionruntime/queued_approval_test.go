package sessionruntime

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/runtime"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestQueuedImageApprovalKeepsOwnerWhileCompactionIsBusy(t *testing.T) {
	h := newSupervisorTurnHarness(t, toolspec.ToolViewImage)
	path := filepath.Join(testsetup.NonTemporaryDirectory(t, "kent-queued-approval-", tools.IsPathInTemporaryDir), "outside.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	busy := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	maintenanceDone := make(chan error, 1)
	go func() {
		maintenanceDone <- h.engine.RunWhenIdle(h.ctx, runtime.ActiveKindCompaction, func() error {
			close(busy)
			select {
			case <-release:
				return nil
			case <-h.ctx.Done():
				return context.Cause(h.ctx)
			}
		})
	}()
	select {
	case <-busy:
	case <-h.ctx.Done():
		t.Fatal("compaction did not acquire the runtime")
	}
	err = h.authority.RunCurrentTurn(h.ctx, mustOpenSessionDescriptor(t, h.sessionID),
		func(commit func() (bool, error)) (bool, error) { return commit() },
		func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
			_, err := engine.QueueUserInputWithAcceptance(ctx, runtime.QueuedUserInput{ExecutionText: "Read the image", CanonicalPresentation: "Read the image"}, accept)
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	current, live := h.authority.SessionExecution(h.sessionID)
	if !live {
		t.Fatal("accepted queued work lost its exact execution owner during compaction")
	}
	unblock()
	if err := <-maintenanceDone; err != nil {
		t.Fatal(err)
	}
	request := h.awaitModel(t, h.main)
	input, _ := json.Marshal(map[string]string{"path": path})
	request.reply <- llm.Response{
		Assistant: llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseCommentary)},
		ToolCalls: []llm.ToolCall{{ID: "image-approval", Name: string(toolspec.ToolViewImage), Input: input}},
		Usage:     llm.Usage{WindowTokens: 200000},
	}
	select {
	case pending := <-h.prompts:
		if pending.scopeID != current.Scope().ID() {
			t.Fatal("approval moved to a different execution")
		}
		results, err := h.authority.ResolvePromptBatch(h.ctx, h.sessionID, pending.stepID, []PromptAnswerCommand{{
			ToolCallID: clientui.ToolCallID(pending.requestID),
			Payload:    PromptApprovalAnswerCommand{Answer: tools.AskQuestionApproval{Decision: tools.AskQuestionApprovalDecisionAllowOnce}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].Outcome != PromptAnswerOutcomeResolved {
			t.Fatalf("approval results: %+v", results)
		}
	case <-h.ctx.Done():
		t.Fatal("outside-workspace image read did not present its approval")
	case unexpected := <-h.main:
		for _, message := range llm.MessagesFromItems(unexpected.request.Items) {
			if message.Role == llm.RoleTool && message.Content != nil {
				t.Fatalf("image read continued without approval: %s", *message.Content)
			}
		}
		t.Fatal("image read continued without a tool result")
	}
	var followup supervisorModelExchange
	finished := make(chan error, 1)
	go func() {
		_, err := current.Wait(h.ctx)
		finished <- err
	}()
	select {
	case followup = <-h.main:
	case err := <-finished:
		t.Fatalf("execution ended before reading image result: %v", err)
	case <-h.ctx.Done():
		t.Fatal("read did not resume")
	}
	imageReturned := false
	for _, item := range followup.request.Items {
		if item.Type != llm.ResponseItemTypeFunctionCallOutput || item.CallID == nil || *item.CallID != "image-approval" {
			continue
		}
		var parts []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item.Output, &parts); err != nil {
			t.Fatalf("approved image result: %s: %v", item.Output, err)
		}
		for _, part := range parts {
			imageReturned = imageReturned || part.Type == "input_image"
		}
	}
	if !imageReturned {
		t.Fatal("approved image was not delivered to the model")
	}
	followup.reply <- supervisorFinalResponse("read complete")
	if _, err := current.Wait(h.ctx); err != nil {
		t.Fatal(err)
	}
}
