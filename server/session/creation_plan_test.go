package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"core/shared/runtimeids"
	"core/shared/textutil"
)

type blockedCloneAppend struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (o *blockedCloneAppend) ObserveEventLogAppend(EventLogAppendObservation) {
	o.once.Do(func() {
		close(o.entered)
		<-o.release
	})
}

func (*blockedCloneAppend) ObserveEventLogSync(EventLogSyncObservation) {}

func TestCommittedCloneReplaysThroughTheOwningSourceStore(t *testing.T) {
	parent := newSessionTestStoreAt(t, t.TempDir())
	parentLog := mustMaterializeSessionTestEventLog(t, parent)
	for range forkReplayFlushEventCount + 1 {
		if _, _, err := parentLog.AppendRecord(nil, userMessagePayload(t, "before clone")); err != nil {
			t.Fatal(err)
		}
	}
	parentMeta := parent.Meta()
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), filepath.Dir(parent.Dir()), "workspace", parentMeta.WorkspaceRoot, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareClone(descriptor, PersistedSessionRecord{SessionDir: parent.Dir(), Meta: &parentMeta}, "child", ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), plan.Snapshot()); err != nil {
		t.Fatal(err)
	}
	foreign := newSessionTestStoreAt(t, t.TempDir())
	if _, err := MaterializeCommittedClone(t.Context(), plan, mustMaterializeSessionTestEventLog(t, foreign), WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)); err == nil {
		t.Fatal("clone accepted another Session's source capability")
	}
	observer := &blockedCloneAppend{entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(observer.release) })
	t.Cleanup(release)
	cloned := make(chan error, 1)
	go func() {
		_, err := MaterializeCommittedClone(t.Context(), plan, parentLog,
			WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence), WithDurabilityObserver(observer))
		cloned <- err
	}()
	select {
	case <-observer.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("clone did not start replay")
	}
	appending := make(chan struct{})
	appended := make(chan error, 1)
	payload := userMessagePayload(t, "after clone")
	go func() {
		close(appending)
		_, _, err := parentLog.AppendRecord(nil, payload)
		appended <- err
	}()
	<-appending
	select {
	case err := <-appended:
		t.Fatalf("source append crossed the ongoing clone replay: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	if err := <-cloned; err != nil {
		t.Fatal(err)
	}
	if err := <-appended; err != nil {
		t.Fatal(err)
	}
	child, err := ResolvePersistedSessionRecord(t.Context(), sessionTestPersistence, descriptor.SessionID().String())
	if err != nil {
		t.Fatal(err)
	}
	if child.Meta.LastSequence != parentMeta.LastSequence || parent.Meta().LastSequence != parentMeta.LastSequence+1 {
		t.Fatalf("replay boundary: child=%d parent=%d original=%d", child.Meta.LastSequence, parent.Meta().LastSequence, parentMeta.LastSequence)
	}
}

func TestCommittedCreationPreservesExistingConversation(t *testing.T) {
	container := t.TempDir()
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), container, "workspace", container, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareCreation(CreationRequest{Descriptor: descriptor})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := plan.Snapshot()
	if _, err := os.Stat(snapshot.SessionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preparation created artifacts: %v", err)
	}
	options := []StoreOption{
		WithPersistenceObserver(sessionTestPersistence),
		WithPersistedSessionResolver(sessionTestPersistence),
	}
	if _, err := MaterializeCommittedCreation(t.Context(), plan, options...); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("uncommitted creation = %v", err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	store, err := MaterializeCommittedCreation(t.Context(), plan, options...)
	if err != nil {
		t.Fatal(err)
	}
	log := mustMaterializeSessionTestEventLog(t, store)
	if _, _, err := log.AppendRecord(nil, userMessagePayload(t, "retain this conversation")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := MaterializeCommittedCreation(t.Context(), plan, options...)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || reopened.Meta().LastSequence != store.Meta().LastSequence {
		t.Fatal("repeated materialization changed committed conversation")
	}
}

func TestLazyCreationCannotTruncateExistingIdentity(t *testing.T) {
	store := newSessionTestStoreAt(t, t.TempDir())
	log := mustMaterializeSessionTestEventLog(t, store)
	if _, _, err := log.AppendRecord(nil, userMessagePayload(t, "established")); err != nil {
		t.Fatal(err)
	}
	meta := store.Meta()
	id, err := runtimeids.ParseSessionID(meta.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := NewLazyWithID(id, filepath.Dir(store.Dir()), meta.WorkspaceContainer, meta.WorkspaceRoot, testSessionCategory, WithPersistenceObserver(sessionTestPersistence))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := duplicate.EnsureDurable(); err == nil {
		t.Fatal("duplicate creation accepted an established identity")
	}
	after, err := os.ReadFile(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("duplicate creation changed established history")
	}
}

func TestCommittedClonePreservesSourceContextAndDoesNotReplayTwice(t *testing.T) {
	parent := newSessionTestStoreAt(t, t.TempDir())
	if err := parent.SetContinuationContext(ContinuationContext{AgentRole: textutil.Value("researcher")}); err != nil {
		t.Fatal(err)
	}
	parentLog := mustMaterializeSessionTestEventLog(t, parent)
	for range forkReplayFlushEventCount + 1 {
		if _, _, err := parentLog.AppendRecord(nil, userMessagePayload(t, "cloned history")); err != nil {
			t.Fatal(err)
		}
	}
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), filepath.Dir(parent.Dir()), "workspace", parent.Meta().WorkspaceRoot, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	parentMeta := parent.Meta()
	plan, err := PrepareClone(descriptor, PersistedSessionRecord{SessionDir: parent.Dir(), Meta: &parentMeta}, "child", ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), plan.Snapshot()); err != nil {
		t.Fatal(err)
	}
	options := []StoreOption{WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)}
	for range 2 {
		child, err := MaterializeCommittedClone(t.Context(), plan, parentLog, options...)
		if err != nil {
			t.Fatal(err)
		}
		meta := child.Meta()
		if meta.LastSequence != parent.Meta().LastSequence ||
			meta.PreviousSessionID == nil || meta.PreviousSessionID.String() != parent.Meta().SessionID ||
			meta.Continuation == nil || meta.Continuation.AgentRole == nil || *meta.Continuation.AgentRole != "researcher" {
			t.Fatalf("clone lost context or duplicated history: %+v", meta)
		}
		if _, err := child.MaterializeEventLog(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommittedCreationRejectsDamagedEstablishedArtifacts(t *testing.T) {
	for _, damage := range []string{"missing", "malformed", "symlink", "incomplete"} {
		t.Run(damage, func(t *testing.T) {
			container := t.TempDir()
			descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), container, "workspace", container, testSessionCategory)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareCreation(CreationRequest{Descriptor: descriptor})
			if err != nil {
				t.Fatal(err)
			}
			options := []StoreOption{WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)}
			store, err := MaterializeCreation(t.Context(), plan, options...)
			if err != nil {
				t.Fatal(err)
			}
			log := mustMaterializeSessionTestEventLog(t, store)
			if _, _, err := log.AppendRecord(nil, userMessagePayload(t, "preserve")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Dir(), eventsFile)
			switch damage {
			case "missing":
				err = os.Rename(path, path+".retained")
			case "malformed":
				err = os.WriteFile(path, []byte("invalid\n"), 0o644)
			case "symlink":
				if err = os.Rename(path, path+".retained"); err == nil {
					err = os.Symlink(path+".retained", path)
				}
			case "incomplete":
				var fp *os.File
				fp, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err == nil {
					_, err = fp.WriteString("{")
					err = errors.Join(err, fp.Close())
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, beforeErr := os.ReadFile(path)
			if _, err := MaterializeCommittedCreation(t.Context(), plan, options...); err == nil {
				t.Fatal("damaged established artifact was accepted")
			}
			after, afterErr := os.ReadFile(path)
			if string(before) != string(after) || errors.Is(beforeErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) {
				t.Fatal("failed materialization changed the damaged artifact")
			}
		})
	}
}

type failedCloneObserver struct{}

func (failedCloneObserver) ObservePersistedStore(context.Context, PersistedStoreSnapshot) error {
	return os.ErrPermission
}

func (failedCloneObserver) ObserveEventLogReconciliation(ctx context.Context, reconciliation PersistedEventLogReconciliation) error {
	return sessionTestPersistence.ObserveEventLogReconciliation(ctx, reconciliation)
}

func TestCommittedCloneDoesNotHideFailedMetadataPublication(t *testing.T) {
	parent := newSessionTestStoreAt(t, t.TempDir())
	log := mustMaterializeSessionTestEventLog(t, parent)
	if _, _, err := log.AppendRecord(nil, userMessagePayload(t, "source")); err != nil {
		t.Fatal(err)
	}
	meta := parent.Meta()
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), filepath.Dir(parent.Dir()), meta.WorkspaceContainer, meta.WorkspaceRoot, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareClone(descriptor, PersistedSessionRecord{SessionDir: parent.Dir(), Meta: &meta}, "clone", ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), plan.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeCommittedClone(t.Context(), plan, log, WithPersistenceObserver(failedCloneObserver{}), WithPersistedSessionResolver(sessionTestPersistence)); err == nil {
		t.Fatal("clone hid publication failure")
	}
	if _, err := os.Stat(filepath.Join(plan.Snapshot().SessionDir, eventsFile)); err != nil {
		t.Fatalf("clone failed before artifact publication: %v", err)
	}
	if _, err := MaterializeCommittedClone(t.Context(), plan, log, WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)); err == nil {
		t.Fatal("retry accepted clone without its committed derived metadata")
	}
}

func TestConcurrentCommittedCreationUsesOneArtifact(t *testing.T) {
	container := t.TempDir()
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), container, "workspace", container, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareCreation(CreationRequest{Descriptor: descriptor})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), plan.Snapshot()); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if _, err := MaterializeCommittedCreation(t.Context(), plan, WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
}

func TestCommittedCreationDoesNotAdoptIncompleteEmptyFile(t *testing.T) {
	container := t.TempDir()
	descriptor, err := NewCreateSessionDescriptor(runtimeids.NewSessionID(), container, "workspace", container, testSessionCategory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareCreation(CreationRequest{Descriptor: descriptor})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionTestPersistence.ObservePersistedStore(t.Context(), plan.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plan.Snapshot().SessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(plan.Snapshot().SessionDir, eventsFile)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeCommittedCreation(t.Context(), plan, WithPersistenceObserver(sessionTestPersistence), WithPersistedSessionResolver(sessionTestPersistence)); err == nil {
		t.Fatal("creation adopted an incomplete empty artifact")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("creation changed incomplete artifact: %+v, %v", info, err)
	}
}
