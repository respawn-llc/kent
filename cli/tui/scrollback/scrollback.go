package scrollback

// You MAY NOT ADD OR REMOVE ANY SINGLE FUNCTION, PROPERTY, OR IMPLEMENTATION OF THIS INTERFACE. Changing the signature of this interface IS NOT ALLOWED and will NOT PASS CODE REVIEW. Changing function signatures is allowed.
type NativeScrollbackBuffer interface {
	Steer(line string) error
	StreamMarkdownAssistantContent(ansi string) error
	FinishAssistantStreaming() error
}
