package sessionview

import (
	"context"
	"errors"
	"reflect"
	"testing"

	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/serverapi"
)

type promptHistoryReaderStub struct {
	history []string
	err     error
}

func (r promptHistoryReaderStub) ReadPromptHistory(context.Context, string) ([]string, error) {
	return r.history, r.err
}

func TestPromptHistoryReadWithoutRuntime(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
	history := make([]string, serverapi.SessionPromptHistoryMaxEntries)
	for i := range history {
		history[i] = "prompt"
	}
	service := NewService(newTestSessionResolver(store), nil, nil).
		WithPromptHistoryReader(promptHistoryReaderStub{history: history})
	result, err := service.GetPromptHistory(t.Context(), &sessionpb.PromptHistoryRequest{SessionId: store.Meta().SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Prompts, history) {
		t.Fatalf("history = %v", result.Prompts)
	}
}

func TestPromptHistoryReadSurfacesFailure(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
	want := errors.New("history unavailable")
	service := NewService(newTestSessionResolver(store), nil, nil).
		WithPromptHistoryReader(promptHistoryReaderStub{err: want})
	result, err := service.GetPromptHistory(t.Context(), &sessionpb.PromptHistoryRequest{SessionId: store.Meta().SessionID})
	if !errors.Is(err, want) || result != nil {
		t.Fatalf("history result = %v, error = %v", result, err)
	}
}
