package authinteraction

import (
	"testing"

	"builder/server/auth"
)

func TestInteractiveNeedsInteractionForRequiredAuthAndEnvConflict(t *testing.T) {
	if InteractiveNeedsInteraction(Request{
		AuthRequired: true,
		Gate:         auth.StartupGate{Ready: true},
		StoredState:  auth.EmptyState(),
		HasEnvAPIKey: true,
	}) {
		t.Fatal("did not expect ready env-only startup to reopen auth picker")
	}
	if !InteractiveNeedsInteraction(Request{
		Gate: auth.StartupGate{Ready: true},
		StoredState: auth.State{
			Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "x"}},
		},
		HasEnvAPIKey: true,
	}) {
		t.Fatal("expected unresolved env-vs-oauth conflict to require interaction")
	}
	if InteractiveNeedsInteraction(Request{
		Gate: auth.StartupGate{Ready: true},
		StoredState: auth.State{
			Method:              auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "x"}},
			EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved,
		},
		HasEnvAPIKey: true,
	}) {
		t.Fatal("did not expect saved preference to reopen conflict picker")
	}
}

func TestHeadlessNeedsInteractionOnlyForRequiredUnreadyAuth(t *testing.T) {
	if !HeadlessNeedsInteraction(Request{AuthRequired: true}) {
		t.Fatal("expected required unready auth to need headless interaction")
	}
	if HeadlessNeedsInteraction(Request{AuthRequired: true, Gate: auth.StartupGate{Ready: true}}) {
		t.Fatal("did not expect ready auth to need headless interaction")
	}
	if HeadlessNeedsInteraction(Request{Gate: auth.StartupGate{Ready: false}}) {
		t.Fatal("did not expect optional auth to need headless interaction")
	}
}

func TestInteractiveNeedsInteractionForNoAuthSelectionOnlyWhenRequired(t *testing.T) {
	req := Request{
		Gate: auth.StartupGate{Ready: false, Reason: auth.ErrAuthNotConfigured.Error()},
		State: auth.State{
			Scope:               auth.ScopeGlobal,
			Method:              auth.Method{Type: auth.MethodNone},
			EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved,
		},
		PromptOptional: true,
	}
	if InteractiveNeedsInteraction(req) {
		t.Fatal("did not expect optional no-auth preference to reopen auth picker")
	}
	req.AuthRequired = true
	if !InteractiveNeedsInteraction(req) {
		t.Fatal("expected required no-auth preference to require auth picker")
	}
}

func TestShouldClearOnSkip(t *testing.T) {
	if !ShouldClearOnSkip(Request{StoredState: auth.State{Method: auth.Method{Type: auth.MethodOAuth, OAuth: &auth.OAuthMethod{AccessToken: "x"}}}}) {
		t.Fatal("expected configured auth to clear on skip")
	}
	if !ShouldClearOnSkip(Request{StoredState: auth.State{EnvAPIKeyPreference: auth.EnvAPIKeyPreferencePreferSaved}}) {
		t.Fatal("expected saved env preference to clear on skip")
	}
	if ShouldClearOnSkip(Request{StoredState: auth.EmptyState()}) {
		t.Fatal("did not expect empty auth state to clear on skip")
	}
}
