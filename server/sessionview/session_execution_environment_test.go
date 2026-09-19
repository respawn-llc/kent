package sessionview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/session"
	"core/server/worktree"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"

	"google.golang.org/protobuf/types/known/emptypb"
)

type sessionExecutionEnvironmentAuthClient struct {
	response *authpb.Status
	err      error
	calls    int
}

func (c *sessionExecutionEnvironmentAuthClient) GetStatus(
	context.Context,
	*authpb.GetStatusRequest,
) (*authpb.Status, error) {
	c.calls++
	return c.response, c.err
}

type sessionExecutionEnvironmentGitRunner struct {
	output   []byte
	err      error
	exitCode int
}

func (r sessionExecutionEnvironmentGitRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return append([]byte(nil), r.output...), r.err
}

func (r sessionExecutionEnvironmentGitRunner) Run(context.Context, string, ...string) ([]byte, int, error) {
	exitCode := r.exitCode
	if r.err != nil && exitCode == 0 {
		exitCode = 1
	}
	return append([]byte(nil), r.output...), exitCode, r.err
}

type mismatchedSessionStoreResolver struct {
	store *session.Store
}

func (r mismatchedSessionStoreResolver) ResolvePersistedSession(context.Context, string) (session.PersistedSessionRecord, error) {
	meta := r.store.Meta()
	return session.PersistedSessionRecord{SessionDir: r.store.Dir(), Meta: &meta}, nil
}

type failingExecutionTargetResolver struct {
	err error
}

func (r failingExecutionTargetResolver) ResolveSessionExecutionTarget(context.Context, string) (*worktreepb.SessionExecutionTarget, error) {
	return nil, r.err
}

type sessionExecutionEnvironmentFixture struct {
	metadata      *metadata.Store
	store         *session.Store
	sessionID     runtimeids.SessionID
	workspaceRoot string
}

func TestSessionExecutionEnvironmentRejectsMismatchedIdentity(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
	otherID, err := runtimeids.ParseSessionID("other-session")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	service := NewService(mismatchedSessionStoreResolver{store: store}, nil, nil)
	if _, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
		SessionId: otherID.String(),
	}); err == nil {
		t.Fatal("environment read accepted a response for another session")
	}
}

func TestSessionExecutionEnvironmentCompleteResponseIsReadOnly(t *testing.T) {
	fixture := newSessionExecutionEnvironmentFixture(t)
	markSessionExecutionEnvironmentGitRepository(t, fixture.workspaceRoot)
	authClient := &sessionExecutionEnvironmentAuthClient{
		response: &authpb.Status{
			Resolution: &authpb.StatusResolution{
				Resolution: &authpb.StatusResolution_Known{Known: &authpb.StatusFacts{
					Method: authpb.AuthMethod_AUTH_METHOD_NONE,
					Provider: &authpb.ProviderFacts{
						Kind:       authpb.ProviderKind_PROVIDER_KIND_OPENAI,
						Identifier: "openai",
					},
					ConnectionId: "test",
					MethodFacts:  &authpb.StatusFacts_NoAuth{NoAuth: &emptypb.Empty{}},
				}},
			},
			Subscription: &authpb.SubscriptionFacts{},
		},
	}
	service := NewService(newTestSessionResolver(fixture.store), nil, fixture.metadata).
		WithExecutionEnvironmentConfig(config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})}).
		WithExecutionEnvironmentAuth(authClient).
		WithExecutionEnvironmentGit(worktree.NewGitInspector(sessionExecutionEnvironmentGitRunner{
			output: []byte("worktree " + fixture.workspaceRoot + "\nHEAD abc123\nbranch refs/heads/main\n\n"),
		}))

	response := readEnvironmentAndAssertPersistenceUnchanged(t, fixture, service)
	workspace := response.Environment.Workspace.GetAvailable()
	workspaceOK := workspace != nil
	branch := response.Environment.Branch.GetAvailable()
	branchOK := branch != nil
	model := response.Environment.Model.GetAvailable()
	modelOK := model != nil
	authState := response.Environment.Auth.GetAvailable()
	authOK := authState != nil
	if !workspaceOK || workspace.Path != fixture.workspaceRoot {
		t.Fatalf("workspace = %+v/%v, want %q", workspace, workspaceOK, fixture.workspaceRoot)
	}
	if !branchOK || branch.Name != "main" {
		t.Fatalf("branch = %+v/%v, want main", branch, branchOK)
	}
	if !modelOK || model.Name != "gpt-5.6-sol" || model.Provider != "openai" || model.Locked {
		t.Fatalf("model = %+v/%v, want unlocked OpenAI model", model, modelOK)
	}
	if !authOK || authState.Provider != "openai" || authState.Method != sessionpb.ExecutionAuthMethod_EXECUTION_AUTH_METHOD_NONE {
		t.Fatalf("auth = %+v/%v, want explicit OpenAI no-auth", authState, authOK)
	}
	if authClient.calls != 1 {
		t.Fatalf("auth status calls = %d, want 1", authClient.calls)
	}
}

func TestSessionExecutionEnvironmentBranchProjection(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
	sessionID := sessionExecutionEnvironmentSessionID(t, store)
	detachedRoot := t.TempDir()
	markSessionExecutionEnvironmentGitRepository(t, detachedRoot)
	subdirectoryRoot := t.TempDir()
	markSessionExecutionEnvironmentGitRepository(t, subdirectoryRoot)
	subdirectory := filepath.Join(subdirectoryRoot, "pkg")
	if err := os.MkdirAll(subdirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll execution subdirectory: %v", err)
	}
	nonGitRoot := t.TempDir()
	failingRoot := t.TempDir()
	markSessionExecutionEnvironmentGitRepository(t, failingRoot)

	tests := []struct {
		name        string
		target      *worktreepb.SessionExecutionTarget
		runner      sessionExecutionEnvironmentGitRunner
		branch      string
		unavailable sessionpb.ExecutionBranchUnavailableReason
		failed      bool
	}{
		{
			name:   "detached head",
			target: availableSessionExecutionTarget(detachedRoot),
			runner: sessionExecutionEnvironmentGitRunner{
				output: []byte("worktree " + detachedRoot + "\nHEAD abc123\ndetached\n\n"),
			},
			unavailable: sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_DETACHED_HEAD,
		},
		{
			name:   "execution subdirectory",
			target: availableSessionExecutionTarget(subdirectory),
			runner: sessionExecutionEnvironmentGitRunner{
				output: []byte("worktree " + subdirectoryRoot + "\nHEAD abc123\nbranch refs/heads/feature\n\n"),
			},
			branch: "feature",
		},
		{
			name:        "non git workspace",
			target:      availableSessionExecutionTarget(nonGitRoot),
			runner:      sessionExecutionEnvironmentGitRunner{},
			unavailable: sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY,
		},
		{
			name:   "unrelated git failure",
			target: availableSessionExecutionTarget(failingRoot),
			runner: sessionExecutionEnvironmentGitRunner{
				output:   []byte("opaque git diagnostic"),
				err:      errors.New("git exited"),
				exitCode: 128,
			},
			failed: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{target: test.target}).
				WithExecutionEnvironmentConfig(config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})}).
				WithExecutionEnvironmentGit(worktree.NewGitInspector(test.runner))
			response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
				SessionId: sessionID.String(),
			})
			if err != nil {
				t.Fatalf("GetSessionExecutionEnvironment: %v", err)
			}
			if err := protoapi.Validate(response); err != nil {
				t.Fatalf("response validation: %v", err)
			}
			switch {
			case test.branch != "":
				branch := response.Environment.Branch.GetAvailable()
				ok := branch != nil
				if !ok || branch.Name != test.branch {
					t.Fatalf("branch = %+v/%v, want %q", branch, ok, test.branch)
				}
			case test.failed:
				failure := response.Environment.Branch.GetFailed()
				ok := failure != nil
				if !ok || failure.Code != sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE {
					t.Fatalf("branch failure = %+v/%v", failure, ok)
				}
			default:
				reason := response.Environment.Branch.GetUnavailable()
				_, ok := response.Environment.Branch.GetResult().(*sessionpb.ExecutionBranchField_Unavailable)
				if !ok || reason != test.unavailable {
					t.Fatalf("branch unavailable = %q/%v, want %q", reason, ok, test.unavailable)
				}
			}
		})
	}
}

func TestSessionExecutionEnvironmentFieldFailuresRemainIndependent(t *testing.T) {
	store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
	sessionID := sessionExecutionEnvironmentSessionID(t, store)
	workspaceRoot := t.TempDir()
	target := availableSessionExecutionTarget(workspaceRoot)

	t.Run("target resolution failure", func(t *testing.T) {
		service := NewService(
			newTestSessionResolver(store),
			nil,
			failingExecutionTargetResolver{err: errors.New("target unavailable")},
		).WithExecutionEnvironmentConfig(config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})})
		response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
			SessionId: sessionID.String(),
		})
		if err != nil {
			t.Fatalf("GetSessionExecutionEnvironment: %v", err)
		}
		if err := protoapi.Validate(response); err != nil {
			t.Fatalf("response validation: %v", err)
		}
		failure := response.Environment.Workspace.GetFailed()
		failed := failure != nil
		modelAvailable := response.Environment.Model.GetAvailable() != nil
		if !failed || failure.Code != sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE || !modelAvailable {
			t.Fatalf("workspace/model fields = %+v/%v model_available=%v", failure, failed, modelAvailable)
		}
	})

	t.Run("missing worktree", func(t *testing.T) {
		missingRoot := filepath.Join(t.TempDir(), "missing-worktree")
		missingTarget := availableSessionExecutionTarget(missingRoot)
		missingTarget.Worktree = &worktreepb.SessionExecutionWorktreeTarget{
			Id:           "worktree-id",
			Root:         missingRoot,
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING,
		}
		service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{target: missingTarget}).
			WithExecutionEnvironmentConfig(config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})})
		response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
			SessionId: sessionID.String(),
		})
		if err != nil {
			t.Fatalf("GetSessionExecutionEnvironment: %v", err)
		}
		if err := protoapi.Validate(response); err != nil {
			t.Fatalf("response validation: %v", err)
		}
		failure := response.Environment.Workspace.GetFailed()
		workspaceFailed := failure != nil
		branchReason := response.Environment.Branch.GetUnavailable()
		_, branchUnavailable := response.Environment.Branch.GetResult().(*sessionpb.ExecutionBranchField_Unavailable)
		modelAvailable := response.Environment.Model.GetAvailable() != nil
		if !workspaceFailed || failure.Code != sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE ||
			!branchUnavailable || branchReason != sessionpb.ExecutionBranchUnavailableReason_EXECUTION_BRANCH_UNAVAILABLE_REASON_NOT_GIT_REPOSITORY ||
			!modelAvailable {
			t.Fatalf(
				"workspace/branch/model = %+v/%v %q/%v model_available=%v",
				failure,
				workspaceFailed,
				branchReason,
				branchUnavailable,
				modelAvailable,
			)
		}
	})

	t.Run("auth lookup failure", func(t *testing.T) {
		authClient := &sessionExecutionEnvironmentAuthClient{err: errors.New("auth unavailable")}
		service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{target: target}).
			WithExecutionEnvironmentConfig(config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})}).
			WithExecutionEnvironmentAuth(authClient)
		response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
			SessionId: sessionID.String(),
		})
		if err != nil {
			t.Fatalf("GetSessionExecutionEnvironment: %v", err)
		}
		if err := protoapi.Validate(response); err != nil {
			t.Fatalf("response validation: %v", err)
		}
		failure := response.Environment.Auth.GetFailed()
		failed := failure != nil
		workspaceAvailable := response.Environment.Workspace.GetAvailable() != nil
		modelAvailable := response.Environment.Model.GetAvailable() != nil
		if !failed || failure.Code != sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_SOURCE_FAILURE ||
			!workspaceAvailable || !modelAvailable || authClient.calls != 1 {
			t.Fatalf(
				"auth/workspace/model = %+v/%v workspace_available=%v model_available=%v calls=%d",
				failure,
				failed,
				workspaceAvailable,
				modelAvailable,
				authClient.calls,
			)
		}
	})

	t.Run("non kent provider skips global auth", func(t *testing.T) {
		authClient := &sessionExecutionEnvironmentAuthClient{err: errors.New("must not be called")}
		store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
		sessionID := sessionExecutionEnvironmentSessionID(t, store)
		if err := store.MarkModelDispatchLocked(session.LockedContract{
			Model:            "claude-sonnet-4",
			ProviderContract: session.LockedProviderCapabilities{ProviderID: "anthropic"},
		}); err != nil {
			t.Fatal(err)
		}
		service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{target: target}).
			WithExecutionEnvironmentConfig(config.App{Settings: config.Settings{
				Model: "claude-sonnet-4",
			}}).
			WithExecutionEnvironmentAuth(authClient)
		response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
			SessionId: sessionID.String(),
		})
		if err != nil {
			t.Fatalf("GetSessionExecutionEnvironment: %v", err)
		}
		if err := protoapi.Validate(response); err != nil {
			t.Fatalf("response validation: %v", err)
		}
		reason := response.Environment.Auth.GetUnavailable()
		_, unavailable := response.Environment.Auth.GetResult().(*sessionpb.ExecutionAuthField_Unavailable)
		if !unavailable || reason != sessionpb.ExecutionAuthUnavailableReason_EXECUTION_AUTH_UNAVAILABLE_REASON_NOT_APPLICABLE || authClient.calls != 0 {
			t.Fatalf("auth unavailable = %q/%v calls=%d", reason, unavailable, authClient.calls)
		}
	})
}

func TestSessionExecutionEnvironmentModelFieldMapping(t *testing.T) {
	tests := []struct {
		name         string
		app          config.App
		locked       *session.LockedContract
		missing      bool
		invalid      bool
		wantName     string
		wantProvider string
	}{
		{name: "missing configuration", missing: true},
		{name: "invalid configuration", app: config.App{Settings: config.Settings{Model: "provider-unknown-model"}}, invalid: true},
		{
			name: "locked provider",
			app:  config.App{Settings: testsetup.ProviderSettings(config.Settings{Model: "gpt-5.6-sol"})},
			locked: &session.LockedContract{
				Model: "claude-sonnet-4",
				ProviderContract: session.LockedProviderCapabilities{
					ProviderID: "anthropic",
				},
			},
			wantName:     "claude-sonnet-4",
			wantProvider: "anthropic",
		},
		{
			name:         "legacy partial lock",
			app:          config.App{Settings: config.Settings{Model: "claude-sonnet-4"}},
			locked:       &session.LockedContract{Model: "gpt-5.6-sol"},
			wantName:     "gpt-5.6-sol",
			wantProvider: "openai",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newSessionViewStore(t, t.TempDir(), "workspace", t.TempDir())
			sessionID := sessionExecutionEnvironmentSessionID(t, store)
			if test.locked != nil {
				if err := store.MarkModelDispatchLocked(*test.locked); err != nil {
					t.Fatalf("MarkModelDispatchLocked: %v", err)
				}
			}
			service := NewService(newTestSessionResolver(store), nil, nil).
				WithExecutionEnvironmentConfig(test.app)
			response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
				SessionId: sessionID.String(),
			})
			if err != nil {
				t.Fatalf("GetSessionExecutionEnvironment: %v", err)
			}
			if err := protoapi.Validate(response); err != nil {
				t.Fatalf("response validation: %v", err)
			}
			if test.missing {
				reason := response.Environment.Model.GetUnavailable()
				_, ok := response.Environment.Model.GetResult().(*sessionpb.ExecutionModelField_Unavailable)
				if !ok || reason != sessionpb.ExecutionModelUnavailableReason_EXECUTION_MODEL_UNAVAILABLE_REASON_NOT_CONFIGURED {
					t.Fatalf("model unavailable = %q/%v", reason, ok)
				}
				return
			}
			if test.invalid {
				failure := response.Environment.Model.GetFailed()
				ok := failure != nil
				if !ok || failure.Code != sessionpb.ExecutionFieldErrorCode_EXECUTION_FIELD_ERROR_CODE_INVALID_CONFIGURATION {
					t.Fatalf("model failure = %+v/%v", failure, ok)
				}
				return
			}
			model := response.Environment.Model.GetAvailable()
			ok := model != nil
			if !ok || model.Name != test.wantName || model.Provider != test.wantProvider || !model.Locked {
				t.Fatalf("model = %+v/%v, want locked %s/%s", model, ok, test.wantName, test.wantProvider)
			}
		})
	}
}

func sessionExecutionEnvironmentSessionID(t *testing.T, store *session.Store) runtimeids.SessionID {
	t.Helper()
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	return sessionID
}

func availableSessionExecutionTarget(workdir string) *worktreepb.SessionExecutionTarget {
	return &worktreepb.SessionExecutionTarget{
		WorkspaceId:           textutil.Value("workspace-id"),
		WorkspaceName:         "workspace",
		WorkspaceRoot:         workdir,
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		CwdRelpath:            ".",
		EffectiveWorkdir:      workdir,
	}
}

func markSessionExecutionEnvironmentGitRepository(t *testing.T, workspaceRoot string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(workspaceRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir .git: %v", err)
	}
}

func newSessionExecutionEnvironmentFixture(t *testing.T) sessionExecutionEnvironmentFixture {
	t.Helper()
	persistenceRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	store, err := session.Create(
		filepath.Join(persistenceRoot, "projects", binding.ProjectID, "sessions"),
		filepath.Base(binding.CanonicalRoot),
		binding.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	return sessionExecutionEnvironmentFixture{
		metadata:      metadataStore,
		store:         store,
		sessionID:     sessionExecutionEnvironmentSessionID(t, store),
		workspaceRoot: binding.CanonicalRoot,
	}
}

func readEnvironmentAndAssertPersistenceUnchanged(
	t *testing.T,
	fixture sessionExecutionEnvironmentFixture,
	service *Service,
) *sessionpb.ExecutionEnvironmentSuccess {
	t.Helper()
	eventsPath := filepath.Join(fixture.store.Dir(), "events.jsonl")
	beforeEvents, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("ReadFile before: %v", err)
	}
	beforeRecord, err := fixture.metadata.ResolvePersistedSession(t.Context(), fixture.sessionID.String())
	if err != nil {
		t.Fatalf("ResolvePersistedSession before: %v", err)
	}

	response, err := service.GetSessionExecutionEnvironment(t.Context(), &sessionpb.ExecutionEnvironmentRequest{
		SessionId: fixture.sessionID.String(),
	})
	if err != nil {
		t.Fatalf("GetSessionExecutionEnvironment: %v", err)
	}
	if err := protoapi.Validate(response); err != nil {
		t.Fatalf("response validation: %v", err)
	}

	afterEvents, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("ReadFile after: %v", err)
	}
	if !bytes.Equal(afterEvents, beforeEvents) {
		t.Fatal("execution-environment read mutated session events")
	}
	afterRecord, err := fixture.metadata.ResolvePersistedSession(t.Context(), fixture.sessionID.String())
	if err != nil {
		t.Fatalf("ResolvePersistedSession after: %v", err)
	}
	if beforeRecord.SessionDir != afterRecord.SessionDir || !reflect.DeepEqual(beforeRecord.Meta, afterRecord.Meta) {
		t.Fatalf("execution-environment read mutated metadata row: before=%+v after=%+v", beforeRecord, afterRecord)
	}
	return response
}
