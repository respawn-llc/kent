package sessionview

import (
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/session"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func TestTranscriptReadSelectorsPreserveUUID(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store := testsetup.OpenStore(t, root)
	binding, err := store.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.Create(filepath.Join(root, "projects", binding.ProjectID, "sessions"), binding.WorkspaceName, workspace, sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, store, store)
	identity := sess.Identity()
	for _, target := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		result, err := service.GetSessionTranscriptPage(t.Context(), &transcriptpb.PageRequest{SessionId: protoapi.SessionSelectorToProto(target)})
		if err != nil || result.Transcript.SessionId != identity.ID.String() {
			t.Fatalf("read %s: %+v, %v", target, result, err)
		}
	}
}
