package metadata

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"core/server/session"
)

func TestStoredLockedContractPreservesCapabilitiesWithObsoleteCountingFields(t *testing.T) {
	t.Parallel()
	const stored = `{
		"model":"gpt-5",
		"provider_contract":{
			"provider_id":"openai",
			"supports_responses_api":true,
			"supports_responses_compact":true,
			"supports_request_input_token_count":true,
			"has_supports_request_input_token_count":true,
			"supports_prompt_cache_key":true,
			"has_supports_prompt_cache_key":true,
			"supports_native_web_search":true,
			"supports_reasoning_encrypted":true,
			"supports_server_side_context_edit":true,
			"supports_provider_verbosity":false,
			"is_openai_first_party":true
		}
	}`
	verbosity := false
	want := session.LockedContract{
		Model: "gpt-5",
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID:                    "openai",
			SupportsResponsesAPI:          true,
			SupportsResponsesCompact:      true,
			SupportsPromptCacheKey:        true,
			HasSupportsPromptCacheKey:     true,
			SupportsNativeWebSearch:       true,
			SupportsReasoningEncrypted:    true,
			SupportsServerSideContextEdit: true,
			SupportsProviderVerbosity:     &verbosity,
			IsOpenAIFirstParty:            true,
		},
	}
	var decoded session.LockedContract
	if err := unmarshalStoredJSON(stored, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded contract = %#v, want %#v", decoded, want)
	}
	encoded, err := marshalJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded session.LockedContract
	if err := unmarshalStoredJSON(encoded, &reloaded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, want) {
		t.Fatalf("reloaded contract = %#v, want %#v", reloaded, want)
	}
}

func TestSessionMetadataDocumentRoundTripsWorkflowNeutralFields(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(123, 456).UTC()
	document := sessionMetadataDocument{
		WorkspaceRoot:                   "/workspace",
		WorkspaceContainer:              "workspace",
		ConversationEstablished:         true,
		HeadlessActive:                  true,
		CompactionSoonReminderIssued:    true,
		GeneratedRecoveredWarningIssued: true,
		WorktreeReminder:                &session.WorktreeReminderState{Mode: session.WorktreeReminderModeEnter},
		Goal: &session.GoalState{
			ID:        "goal",
			Objective: "finish",
			Status:    session.GoalStatusActive,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		},
		ActiveWorkflowAssignment: &session.MessageRecord{
			Role: session.MessageRoleDeveloper,
		},
		ActiveWorkflowAssignmentState: &session.ActiveWorkflowAssignmentState{},
	}

	encoded, err := marshalJSON(document)
	if err != nil {
		t.Fatalf("marshalJSON: %v", err)
	}
	var encodedFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &encodedFields); err != nil {
		t.Fatalf("decode encoded metadata fields: %v", err)
	}
	if _, ok := encodedFields["prompt_cache_lineage_generation"]; ok {
		t.Fatalf("encoded metadata retained obsolete prompt cache lineage generation: %s", encoded)
	}
	var legacyDecoded sessionMetadataDocument
	if err := unmarshalStoredJSON(`{"workspace_root":"/workspace","prompt_cache_lineage_generation":7}`, &legacyDecoded); err != nil {
		t.Fatalf("unmarshalStoredJSON legacy metadata: %v", err)
	}
	legacyEncoded, err := marshalJSON(legacyDecoded)
	if err != nil {
		t.Fatalf("marshalJSON legacy metadata: %v", err)
	}
	var legacyEncodedFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(legacyEncoded), &legacyEncodedFields); err != nil {
		t.Fatalf("decode rewritten legacy metadata fields: %v", err)
	}
	if _, ok := legacyEncodedFields["prompt_cache_lineage_generation"]; ok {
		t.Fatalf("rewritten metadata retained obsolete prompt cache lineage generation: %s", legacyEncoded)
	}
	var decoded sessionMetadataDocument
	if err := unmarshalStoredJSON(encoded, &decoded); err != nil {
		t.Fatalf("unmarshalStoredJSON: %v", err)
	}
	if !reflect.DeepEqual(decoded, document) {
		t.Fatalf("decoded metadata = %#v, want %#v", decoded, document)
	}
}
