package app

import "core/shared/client"

func newHeadlessRunPromptClient(server *embeddedAppServer) client.RunPromptClient {
	return server.RunPromptClient()
}
