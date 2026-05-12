package serverbridge

import (
	"errors"
	"reflect"
	"testing"

	serverauth "builder/server/auth"
	serverllm "builder/server/llm"
	"builder/server/session"
	sharedauth "builder/shared/auth"
	"builder/shared/config"
	"builder/shared/llmerrors"
	"builder/shared/modelcontract"
	"builder/shared/sessioncontract"
)

func TestBridgeDTOsUseSharedContracts(t *testing.T) {
	assertSameType[serverauth.State, sharedauth.State](t)
	assertSameType[serverauth.Method, sharedauth.Method](t)
	assertSameType[serverauth.StartupGate, sharedauth.StartupGate](t)
	assertSameType[serverllm.APIStatusError, llmerrors.APIStatusError](t)
	assertSameType[serverllm.ProviderAPIError, llmerrors.ProviderAPIError](t)
	assertSameType[serverllm.ProviderCapabilities, modelcontract.ProviderCapabilities](t)
	assertSameType[serverllm.ModelMetadata, modelcontract.ModelMetadata](t)

	if !errors.Is(session.ErrSessionNotFound, sessioncontract.ErrSessionNotFound) {
		t.Fatal("server session not-found sentinel must be shared session contract sentinel")
	}
}

func TestModelBridgeFunctionsExposeSharedDTOs(t *testing.T) {
	var providerFn func(sharedauth.State, config.Settings) (modelcontract.ProviderCapabilities, error) = ProviderCapabilitiesForSettings
	var metadataFn func(string) (modelcontract.ModelMetadata, bool) = LookupModelMetadata

	if _, err := providerFn(sharedauth.State{Method: sharedauth.Method{Type: sharedauth.MethodAPIKey, APIKey: &sharedauth.APIKeyMethod{Key: "sk-test"}}}, config.Settings{}); err != nil {
		t.Fatalf("provider capabilities through shared DTO contract: %v", err)
	}
	if _, ok := metadataFn("gpt-5.3-codex"); !ok {
		t.Fatal("expected model metadata lookup through shared DTO contract")
	}
}

func assertSameType[A any, B any](t *testing.T) {
	t.Helper()
	typeA := reflect.TypeOf((*A)(nil)).Elem()
	typeB := reflect.TypeOf((*B)(nil)).Elem()
	if typeA != typeB {
		t.Fatalf("types differ: %s != %s", typeA, typeB)
	}
}
