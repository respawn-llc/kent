package app

import (
	"io"

	"builder/server/launch"
	"builder/server/runprompt"
	"builder/server/runtime"
	"builder/server/session"
	"builder/server/tools/askquestion"
	"builder/shared/client"
	"builder/shared/serverapi"
)

func newHeadlessRunPromptClient(server *embeddedAppServer) client.RunPromptClient {
	target, err := runPromptTargetForEmbeddedAttachment(server)
	if err != nil {
		panic(err)
	}
	return target.Value.Client
}

func ensureSubagentSessionName(store *session.Store) error {
	return launch.EnsureSubagentSessionName(store)
}

func runPromptAskHandler(req askquestion.Request) (askquestion.Response, error) {
	return runprompt.RunPromptAskHandler(req)
}

func writeRunProgressEvent(w io.Writer, evt runtime.Event) {
	publishRunPromptProgress(runPromptIOProgressSink{writer: w}, evt)
}

func publishRunPromptProgress(progress serverapi.RunPromptProgressSink, evt runtime.Event) {
	runprompt.PublishRunPromptProgress(progress, evt)
}
