package tui

type transcriptMessageStyle uint8

const (
	transcriptMessageStyleNone transcriptMessageStyle = iota
	transcriptMessageStyleSuccess
	transcriptMessageStyleWarning
	transcriptMessageStyleError
)

func transcriptMessageStyleForIntent(intent RenderIntent) transcriptMessageStyle {
	switch intent {
	case RenderIntentReviewerStatus, RenderIntentReviewerSuggestions:
		return transcriptMessageStyleSuccess
	case RenderIntentWarning, RenderIntentCacheWarning:
		return transcriptMessageStyleWarning
	case RenderIntentError, RenderIntentDeveloperErrorFeedback:
		return transcriptMessageStyleError
	default:
		return transcriptMessageStyleNone
	}
}

func transcriptMessageStyleSymbol(style transcriptMessageStyle) string {
	switch style {
	case transcriptMessageStyleSuccess:
		return "§"
	case transcriptMessageStyleWarning:
		return "⚠"
	case transcriptMessageStyleError:
		return "!"
	default:
		return ""
	}
}
