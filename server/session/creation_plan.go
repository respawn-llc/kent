package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/shared/runtimeids"
	"core/shared/textutil"
)

// CreationPlan grants artifact creation only for one newly allocated identity.
// Opening an ordinary persisted Session never grants this capability.
type CreationPlan struct {
	descriptor SessionDescriptor
	snapshot   PersistedStoreSnapshot
	clone      *cloneCreation
}

type cloneCreation struct {
	source   SessionDescriptor
	thinking ForkThinking
}

// PrepareClone captures source identity and metadata without opening history.
// Startup acquires the source Store for the existing bounded replay.
func PrepareClone(descriptor SessionDescriptor, source PersistedSessionRecord, name string, thinking ForkThinking, options ...StoreOption) (CreationPlan, error) {
	if err := ValidateOriginalThinkingEffort(&thinking.Desired); err != nil {
		return CreationPlan{}, err
	}
	if source.Meta == nil {
		return CreationPlan{}, errResolverRecordMissingMetadata
	}
	sourceID, err := runtimeids.ParseSessionID(source.Meta.SessionID)
	if err != nil {
		return CreationPlan{}, err
	}
	if err := descriptor.Validate(); err != nil {
		return CreationPlan{}, err
	}
	if descriptor.SessionID() == sourceID {
		return CreationPlan{}, errors.New("clone source and child identities must differ")
	}
	if err := validatePersistedSessionRecord(sourceID.String(), source); err != nil {
		return CreationPlan{}, err
	}
	if filepath.Base(source.SessionDir) != sourceID.String() {
		return CreationPlan{}, errResolverRecordSessionDirMismatch
	}
	plan, err := PrepareCreation(CreationRequest{
		Descriptor: descriptor, Source: CreationContextSourceFromMeta(*source.Meta),
		SourceKind:     SessionCreationSourcePreviousSession,
		ContextOptions: ChildContextOptions{LockedContract: InheritFullContract, InheritContinuation: true},
	}, options...)
	if err != nil {
		return CreationPlan{}, err
	}
	plan.snapshot.Meta.Name = strings.TrimSpace(name)
	plan.snapshot.Meta.ChatSettings = &ChatSettingsOverrides{Thinking: textutil.Value(thinking.Desired)}
	if thinking.PreserveNativeUpdates {
		plan.snapshot.Meta.OriginalThinkingEffort = textutil.Pointer(source.Meta.OriginalThinkingEffort)
	}
	sourceDescriptor, err := NewScopedOpenSessionDescriptor(sourceID, filepath.Dir(source.SessionDir))
	if err != nil {
		return CreationPlan{}, err
	}
	plan.clone = &cloneCreation{source: sourceDescriptor, thinking: thinking}
	return plan, nil
}

type CreationRequest struct {
	Descriptor     SessionDescriptor
	Source         CreationContextSource
	SourceKind     SessionCreationSourceKind
	ContextOptions ChildContextOptions
	InitialChat    *ChatDraftState
}

// PrepareCreation initializes identity and inherited facts without publishing
// metadata or touching the filesystem.
func PrepareCreation(request CreationRequest, options ...StoreOption) (CreationPlan, error) {
	value, ok := request.Descriptor.value.(createSessionDescriptor)
	if !ok {
		return CreationPlan{}, errors.New("Session creation requires a create descriptor")
	}
	store, err := NewLazyWithID(value.sessionID, value.containerDir, value.containerName, value.workspaceRoot, value.category, options...)
	if err != nil {
		return CreationPlan{}, err
	}
	if err := InitializeCreationContext(store, request.Source, request.SourceKind, request.ContextOptions); err != nil {
		return CreationPlan{}, err
	}
	if request.InitialChat != nil {
		if err := InitializeChatDraft(store, *request.InitialChat); err != nil {
			return CreationPlan{}, err
		}
	}
	return CreationPlan{
		descriptor: request.Descriptor,
		snapshot: PersistedStoreSnapshot{
			SessionDir:   store.Dir(),
			Meta:         store.Meta(),
			ContextFacts: store.initialContextFacts.Clone(),
		},
	}, nil
}

func (p CreationPlan) Descriptor() SessionDescriptor { return p.descriptor }

func (p CreationPlan) SourceDescriptor() *SessionDescriptor {
	if p.clone == nil {
		return nil
	}
	source := p.clone.source
	return &source
}

func (p CreationPlan) Snapshot() PersistedStoreSnapshot {
	return PersistedStoreSnapshot{
		SessionDir:   p.snapshot.SessionDir,
		Meta:         cloneMeta(p.snapshot.Meta),
		ContextFacts: p.snapshot.ContextFacts.Clone(),
	}
}

// MaterializeCreation publishes an ordinary newly planned Session through its
// existing metadata owner before installing the same committed artifacts.
func MaterializeCreation(ctx context.Context, plan CreationPlan, options ...StoreOption) (*Store, error) {
	if plan.clone != nil {
		return nil, errors.New("clone materialization requires its owning source event log")
	}
	if err := publishCreationMetadata(ctx, plan, options...); err != nil {
		return nil, err
	}
	return MaterializeCommittedCreation(ctx, plan, options...)
}

func publishCreationMetadata(ctx context.Context, plan CreationPlan, options ...StoreOption) error {
	if err := plan.descriptor.Validate(); err != nil {
		return err
	}
	opts := normalizeStoreOptions(options...)
	if opts.observer == nil {
		return errPersistenceObserverRequired
	}
	if _, err := ResolvePersistedSessionRecord(ctx, opts.resolver, plan.descriptor.SessionID().String()); !errors.Is(err, ErrSessionNotFound) {
		if err == nil {
			return errors.New("new Session identity already exists")
		}
		return err
	}
	if err := opts.observer.ObservePersistedStore(ctx, plan.Snapshot()); err != nil {
		return err
	}
	return nil
}

// WithLaunchMetadata completes prompt-facing metadata while it is still private.
func (p CreationPlan) WithLaunchMetadata(name *string, continuation ContinuationContext) (CreationPlan, error) {
	if err := p.descriptor.Validate(); err != nil {
		return CreationPlan{}, err
	}
	normalized, err := NormalizeContinuationContext(continuation)
	if err != nil {
		return CreationPlan{}, err
	}
	p.snapshot = p.Snapshot()
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return CreationPlan{}, errors.New("Session name cannot be blank")
		}
		p.snapshot.Meta.Name = strings.TrimSpace(*name)
	}
	p.snapshot.Meta.Continuation = normalized
	return p, nil
}

func (p CreationPlan) WithListingMetadata(name, firstPromptPreview string) (CreationPlan, error) {
	if err := p.descriptor.Validate(); err != nil {
		return CreationPlan{}, err
	}
	p.snapshot = p.Snapshot()
	p.snapshot.Meta.Name = strings.TrimSpace(name)
	p.snapshot.Meta.FirstPromptPreview = normalizeFirstPromptPreview(firstPromptPreview)
	return p, nil
}

func (p CreationPlan) WithWorktreeReminder(reminder *WorktreeReminderState) (CreationPlan, error) {
	if err := p.descriptor.Validate(); err != nil {
		return CreationPlan{}, err
	}
	p.snapshot = p.Snapshot()
	p.snapshot.Meta.WorktreeReminder = CloneWorktreeReminderState(reminder)
	if err := normalizeMetaWorktreeReminder(&p.snapshot.Meta); err != nil {
		return CreationPlan{}, err
	}
	return p, nil
}

// MaterializeCommittedCreation installs fresh artifacts only after metadata has
// committed. Repeated calls open the current authoritative record, never replay
// the plan's old metadata over an established Session.
func MaterializeCommittedCreation(ctx context.Context, plan CreationPlan, options ...StoreOption) (*Store, error) {
	if plan.clone != nil {
		return nil, errors.New("clone materialization requires its owning source event log")
	}
	return materializeCommittedCreation(ctx, plan, nil, options...)
}

func MaterializeClone(ctx context.Context, plan CreationPlan, source MaterializedEventLog, options ...StoreOption) (*Store, error) {
	if err := validateCloneSource(plan, source); err != nil {
		return nil, err
	}
	if err := publishCreationMetadata(ctx, plan, options...); err != nil {
		return nil, err
	}
	return MaterializeCommittedClone(ctx, plan, source, options...)
}

func MaterializeCommittedClone(ctx context.Context, plan CreationPlan, source MaterializedEventLog, options ...StoreOption) (*Store, error) {
	if err := validateCloneSource(plan, source); err != nil {
		return nil, err
	}
	return materializeCommittedCreation(ctx, plan, &source, options...)
}

func validateCloneSource(plan CreationPlan, source MaterializedEventLog) error {
	if plan.clone == nil {
		return errors.New("clone materialization requires a clone creation plan")
	}
	parent, err := materializedForkParent(source)
	if err != nil {
		return err
	}
	if parent.Meta().SessionID != plan.clone.source.SessionID().String() {
		return errors.New("clone source does not match the planned Session")
	}
	scoped := plan.clone.source.value.(openSessionDescriptor)
	return validatePersistedSessionDir(filepath.Join(*scoped.containerDir, scoped.sessionID.String()), parent.Dir())
}

func materializeCommittedCreation(ctx context.Context, plan CreationPlan, source *MaterializedEventLog, options ...StoreOption) (*Store, error) {
	if err := plan.descriptor.Validate(); err != nil {
		return nil, err
	}
	opts := normalizeStoreOptions(options...)
	record, err := ResolvePersistedSessionRecord(ctx, opts.resolver, plan.descriptor.SessionID().String())
	if err != nil {
		return nil, err
	}
	if err := validatePersistedSessionDir(plan.snapshot.SessionDir, record.SessionDir); err != nil {
		return nil, err
	}
	// SQLite stores milliseconds; an in-memory plan can retain finer precision.
	if record.Meta.CreatedAt.UnixMilli() != plan.snapshot.Meta.CreatedAt.UnixMilli() {
		return nil, errors.New("committed Session identity does not match creation plan")
	}
	if err := materializeCreationArtifacts(ctx, plan, record, source, opts); err != nil {
		return nil, err
	}
	return Open(record.SessionDir, options...)
}

func materializeCreationArtifacts(ctx context.Context, plan CreationPlan, record PersistedSessionRecord, source *MaterializedEventLog, opts storeOptions) (resultErr error) {
	eventsPath := filepath.Join(record.SessionDir, eventsFile)
	_, err := os.Lstat(eventsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) && (record.Meta.LastSequence != 0 || record.Meta.ConversationEstablished) {
		return errors.New("established Session event log is missing")
	}
	if err := os.MkdirAll(record.SessionDir, 0o755); err != nil {
		return err
	}
	lock, lockPath, err := acquireEventLogPersistenceLock(record.SessionDir)
	if err != nil {
		return err
	}
	defer joinEventLogPersistenceLockRelease(&resultErr, lock, lockPath)
	// Refresh under the existing artifact lock: a different materializer may
	// have finished and appended records while this invocation was waiting.
	record, err = ResolvePersistedSessionRecord(ctx, opts.resolver, plan.descriptor.SessionID().String())
	if err != nil {
		return err
	}
	if err := validatePersistedSessionDir(plan.snapshot.SessionDir, record.SessionDir); err != nil {
		return err
	}
	if info, err := os.Lstat(eventsPath); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("Session event log must be a regular file")
		}
		if info.Size() == 0 {
			return errors.New("created Session event log is empty")
		}
		log, err := openCurrentEventLog(eventsPath, currentEventLogPersistedSnapshot)
		if err != nil {
			return err
		}
		if log.boundaryIncomplete || log.lastSequence != record.Meta.LastSequence {
			return errors.New("created Session event log does not match committed metadata")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if record.Meta.LastSequence != 0 || record.Meta.ConversationEstablished {
		return errors.New("established Session event log is missing")
	}
	stagingDir, err := os.MkdirTemp(record.SessionDir, ".creation-")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(stagingDir)) }()
	stagedPath := filepath.Join(stagingDir, eventsFile)
	version := EventLogVersionV2
	if plan.clone != nil {
		version = source.log.version
	}
	log, err := createCurrentEventLogVersion(stagedPath, version)
	if err != nil {
		return err
	}
	log.durabilityObserver = opts.durabilityObserver
	var cloneSnapshot *PersistedStoreSnapshot
	if plan.clone != nil {
		if opts.observer == nil {
			return errPersistenceObserverRequired
		}
		// The staging Store is private metadata accumulation, not durable state.
		staged := &Store{meta: cloneMeta(*record.Meta)}
		derived, _, err := streamReplay(*source, version, func(records []EventRecord) (recordAppendOutcome, error) {
			inputs, err := replayRecordInputs(records)
			if err != nil {
				return recordAppendOutcome{}, err
			}
			projected, err := buildAppendRecords(inputs, version, log.lastSequence, storeTimestamp(opts))
			if err != nil {
				return recordAppendOutcome{}, err
			}
			if err := staged.advanceAppendedRecordMetadataLocked(projected); err != nil {
				return recordAppendOutcome{}, err
			}
			end, err := log.appendRecords(projected)
			return recordAppendOutcome{records: projected, endByteCursor: &end}, err
		}, 0, plan.clone.thinking.PreserveNativeUpdates)
		if err != nil {
			return err
		}
		staged.meta.LastSequence = log.lastSequence
		staged.meta.HeadlessActive = derived.headlessActive
		staged.meta.CompactionSoonReminderIssued = derived.reminderIssued
		cloneSnapshot = &PersistedStoreSnapshot{SessionDir: record.SessionDir, Meta: staged.meta, ContextFacts: record.ContextFacts}
	}
	// Link installs the completed file atomically without replacing anything
	// another writer may have created. The staging link is then disposable.
	if err := os.Link(stagedPath, eventsPath); err != nil {
		return fmt.Errorf("publish created Session event log: %w", err)
	}
	if cloneSnapshot != nil {
		return opts.observer.ObservePersistedStore(ctx, *cloneSnapshot)
	}
	return nil
}
