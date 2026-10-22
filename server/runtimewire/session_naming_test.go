package runtimewire

import (
	"context"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

func TestNamingSubagentResumeContext(t *testing.T) {
	for _, missingPreview := range []bool{false, true} {
		t.Run(map[bool]string{false: "assignment preview", true: "historical missing preview"}[missingPreview], func(t *testing.T) {
			root := t.TempDir()
			store := newRuntimeWireSession(t, root, "subagent")
			active := config.DefaultOnboardingSettings()
			active.Model = "gpt-6-sol"
			active.ModelContextWindow = 200000
			active.ContextCompactionThresholdTokens = 180000
			active.Reviewer.Frequency = "off"
			submitted := make(chan runtime.SessionNamingInput, 1)
			wire := func() *RuntimeWiring {
				t.Helper()
				factory := RuntimeClientFactoryFunc(func(context.Context, RuntimeClientRequest) (llm.Client, error) {
					return &runtimewireCaptureClient{responses: []llm.Response{{Assistant: llm.Message{
						Role: llm.RoleAssistant, Content: textutil.Value("Done"), Phase: textutil.Value(llm.MessagePhaseFinal),
					}}}}, nil
				})
				wiring, err := newTestRuntimeWiringWithBackground(t, store, materializedRuntimeWireEventLog(t, store),
					active, nil, nil, nil, nil, requiredRuntimeWireTestOptions(RuntimeWiringOptions{
						FilesystemContext: runtimeWireFilesystemContext(t, root), ClientFactory: factory,
						SubmitSessionNaming: func(input runtime.SessionNamingInput) { submitted <- input },
					}))
				if err != nil {
					t.Fatal(err)
				}
				return wiring
			}
			initial := wire()
			assignment := runtime.NewAgentSteer(runtimeids.SessionIdentity{ID: runtimeids.NewSessionID()}, "Fix invoice totals")
			if _, err := initial.Engine.SubmitAgentSteerWithHooks(t.Context(), assignment, nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := initial.Close(); err != nil {
				t.Fatal(err)
			}
			// Wiring captures the first Step independently of the current nano
			// availability; the server consumer resolves settings at submission.
			<-submitted
			if missingPreview {
				if err := store.SetListingMetadata(nil, ""); err != nil {
					t.Fatal(err)
				}
			} else if store.Meta().FirstPromptPreview != "Fix invoice totals" {
				t.Fatalf("assignment preview = %q", store.Meta().FirstPromptPreview)
			}
			active.Nano = &config.NanoSettings{
				Connection: config.SingleConnection("test"), Model: textutil.Value("gpt-6-sol"),
				TimeoutSeconds: 120, SessionNaming: true,
			}
			resumed := wire()
			t.Cleanup(func() { _ = resumed.Close() })
			if _, err := resumed.Engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
				t.Fatal(err)
			}
			select {
			case input := <-submitted:
				if missingPreview || input.Task != "Fix invoice totals" {
					t.Fatalf("resume naming input = %+v", input)
				}
			default:
				if !missingPreview {
					t.Fatal("resumed assignment was not named")
				}
			}
			if missingPreview && store.Meta().FirstPromptPreview != "" {
				t.Fatal("continuation became the historical task preview")
			}
		})
	}
}
