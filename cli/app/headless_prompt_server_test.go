package app

import "core/shared/client"

func newHeadlessRunPromptClient(server *embeddedAppServer) client.RunPromptClient {
	target, err := runPromptTargetForEmbeddedAttachment(server)
	if err != nil {
		panic(err)
	}
	return target.Value.Client
}
