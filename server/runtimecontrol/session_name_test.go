package runtimecontrol

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/runtime"
	"core/server/session"
	"core/server/sessionruntime"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSetSessionNamePersistsWithoutStartingDormantRuntime(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	metadataStore, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Create(
		filepath.Join(root, "projects", binding.ProjectID, "sessions"),
		binding.WorkspaceName, workspace, sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{
		PersistenceRoot: root,
		StoreOptions:    metadataStore.AuthoritativeSessionStoreOptions(),
	})
	t.Cleanup(func() {
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service := NewService(authority)
	request := &runtimepb.SetSessionNameRequest{
		SessionId: id.String(),
		Mutation: &runtimepb.SessionNameMutation{
			Action: &runtimepb.SessionNameMutation_Set{Set: "  レビュー  \"release\"\nnotes  "},
		},
	}
	if err := service.SetSessionName(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	record, err := metadataStore.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.Name == nil || *record.Meta.Name != "レビュー  \"release\"\nnotes" {
		t.Fatalf("persisted name = %v, want literal interior text", record.Meta.Name)
	}
	request.Mutation.Action = &runtimepb.SessionNameMutation_Set{Set: " \n\t "}
	if err := service.SetSessionName(t.Context(), request); err == nil {
		t.Fatal("blank Set accepted")
	}
	record, err = metadataStore.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.Name == nil || *record.Meta.Name != "レビュー  \"release\"\nnotes" {
		t.Fatal("rejected Set changed the authoritative name")
	}
	request.Mutation.Action = &runtimepb.SessionNameMutation_Clear{Clear: &emptypb.Empty{}}
	if err := service.SetSessionName(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	record, err = metadataStore.ResolvePersistedSession(t.Context(), id.String())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := session.OpenResolved(record, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Meta().Name != nil {
		t.Fatal("Clear did not persist absence")
	}
	if record.Meta.LastSequence != store.Meta().LastSequence {
		t.Fatal("name mutation appended transcript content")
	}
	err = authority.WithCurrentRuntime(t.Context(), id, func(context.Context, *runtime.Engine) error {
		t.Fatal("name mutation started an Agent runtime")
		return nil
	})
	if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("runtime availability = %v, want unavailable", err)
	}
}
