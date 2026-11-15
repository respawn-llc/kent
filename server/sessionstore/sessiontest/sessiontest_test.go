package sessiontest_test

import (
	"testing"

	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/sessioncontract"
)

func TestPersistenceRetainsDistinctSessionIdentities(t *testing.T) {
	persistence := sessiontest.NewPersistence()
	root := t.TempDir()
	first, err := session.Create(root, "workspace", root, sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.Create(root, "workspace", root, sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := first.Identity().ShortID, second.Identity().ShortID
	if firstID == nil || secondID == nil || firstID.Int64() == secondID.Int64() {
		t.Fatalf("expected distinct assigned Session identities: first=%v second=%v", firstID, secondID)
	}
	if err := first.EnsureDurable(); err != nil {
		t.Fatal(err)
	}
	reopened, err := persistence.Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	reopenedID := reopened.Identity().ShortID
	if reopenedID == nil || reopenedID.Int64() != firstID.Int64() {
		t.Fatalf("reopened Session identity changed: got %v want %v", reopenedID, firstID)
	}
	record, err := persistence.ResolvePersistedSession(t.Context(), first.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	*record.ShortID = *secondID
	again, err := persistence.ResolvePersistedSession(t.Context(), first.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ShortID == nil || again.ShortID.Int64() != firstID.Int64() {
		t.Fatalf("mutating resolved record changed authoritative identity: %v", again.ShortID)
	}
}
