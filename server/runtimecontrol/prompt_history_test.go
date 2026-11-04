package runtimecontrol

import (
	"context"
	"errors"
	"slices"
	"testing"

	runtime "core/server/agentruntime"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestRecordPromptHistoryPersistsWithoutRuntime(t *testing.T) {
	store, service := newNumberedRuntimeControlFixture(t)
	t.Cleanup(func() {
		if err := service.authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service.WithPromptHistoryStore(service.metadata)
	id := runtimeids.SessionIDFromProto(store.Meta().SessionID)
	for _, text := range []string{"/agent", "/model"} {
		if err := service.RecordPromptHistory(t.Context(), &promptpb.RecordHistoryRequest{
			SessionId: id.String(), Text: text,
		}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := service.metadata.ReadPromptHistory(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(history, []string{"/agent", "/model"}) {
		t.Fatalf("persisted prompt history = %v", history)
	}
	if err := service.authority.WithCurrentRuntime(t.Context(), id, func(context.Context, *runtime.Engine) error {
		return nil
	}); !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("prompt history save activated a runtime: %v", err)
	}
}

func TestRecordPromptHistoryReportsPersistenceFailureWithoutRuntime(t *testing.T) {
	store, service := newNumberedRuntimeControlFixture(t)
	history := newRuntimeControlPromptHistoryStore(store.Meta().SessionID)
	failure := errors.New("database write failed")
	history.SetRecordError(failure)
	service.WithPromptHistoryStore(history)
	if err := service.RecordPromptHistory(t.Context(), &promptpb.RecordHistoryRequest{
		SessionId: store.Meta().SessionID, Text: "/agent",
	}); !errors.Is(err, failure) {
		t.Fatalf("prompt history write error = %v, want %v", err, failure)
	}
}
