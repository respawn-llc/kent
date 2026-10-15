package transcript

// WebSearchDetail is a transient projection of a saved hosted completion.
type WebSearchDetail struct {
	Action  WebSearchAction
	Results []WebSearchResult
	Sources []string
}

type WebSearchAction interface {
	webSearchAction()
}

type WebSearchSearch struct{ Queries []string }
type WebSearchOpenPage struct{ URL *string }
type WebSearchFindInPage struct {
	URL     *string
	Pattern *string
}

func (WebSearchSearch) webSearchAction()     {}
func (WebSearchOpenPage) webSearchAction()   {}
func (WebSearchFindInPage) webSearchAction() {}

type WebSearchResult struct {
	Title       *string
	Destination *string
	Image       bool
}
