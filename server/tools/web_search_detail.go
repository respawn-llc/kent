package tools

import (
	"encoding/json"
	"fmt"

	"core/shared/transcript"
)

type hostedWebSearchResult struct {
	Type             string  `json:"type"`
	Title            *string `json:"title"`
	URL              *string `json:"url"`
	ImageURL         *string `json:"image_url"`
	SourceWebsiteURL *string `json:"source_website_url"`
}

// DecodeWebSearchDetail derives display facts without changing provider history.
func DecodeWebSearchDetail(raw json.RawMessage) (*transcript.WebSearchDetail, error) {
	var payload hostedWebSearchPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode web search: %w", err)
	}
	detail := &transcript.WebSearchDetail{}
	queries, err := decodeWebSearchQueries(payload.Action.Queries)
	if err != nil {
		return nil, err
	}
	if err := validateWebSearchStrings(payload.Action.URL, payload.Action.Pattern); err != nil {
		return nil, err
	}
	for _, query := range queries {
		if err := validateWebSearchStrings(&query); err != nil {
			return nil, err
		}
	}
	switch payload.Action.Type {
	case "search":
		detail.Action = transcript.WebSearchSearch{Queries: queries}
	case "open_page":
		detail.Action = transcript.WebSearchOpenPage{URL: payload.Action.URL}
	case "find_in_page":
		detail.Action = transcript.WebSearchFindInPage{URL: payload.Action.URL, Pattern: payload.Action.Pattern}
	default:
		return nil, fmt.Errorf("unknown web search action %q", payload.Action.Type)
	}
	var results []hostedWebSearchResult
	if len(payload.Results) > 0 {
		if err := json.Unmarshal(payload.Results, &results); err != nil {
			return nil, fmt.Errorf("decode web search results: %w", err)
		}
	}
	for _, result := range results {
		if err := validateWebSearchStrings(result.Title, result.URL, result.ImageURL, result.SourceWebsiteURL); err != nil {
			return nil, err
		}
		destination := result.URL
		image := result.Type == "image_result"
		if image {
			destination = result.ImageURL
			if destination == nil {
				destination = result.SourceWebsiteURL
			}
		}
		if result.Title != nil || destination != nil {
			detail.Results = append(detail.Results, transcript.WebSearchResult{
				Title: result.Title, Destination: destination, Image: image,
			})
		}
	}
	var sources []struct {
		URL *string `json:"url"`
	}
	if len(payload.Action.Sources) > 0 {
		if err := json.Unmarshal(payload.Action.Sources, &sources); err != nil {
			return nil, fmt.Errorf("decode web search sources: %w", err)
		}
	}
	for _, source := range sources {
		if err := validateWebSearchStrings(source.URL); err != nil {
			return nil, err
		}
		if source.URL != nil {
			detail.Sources = append(detail.Sources, *source.URL)
		}
	}
	usefulPage := payload.Action.Type == "open_page" && payload.Action.URL != nil ||
		payload.Action.Type == "find_in_page" && (payload.Action.URL != nil || payload.Action.Pattern != nil)
	if len(detail.Results) == 0 && len(detail.Sources) == 0 && !usefulPage {
		return nil, nil
	}
	return detail, nil
}

func decodeWebSearchQueries(raw json.RawMessage) ([]string, error) {
	var queries []string
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &queries); err != nil {
			return nil, fmt.Errorf("decode web search queries: %w", err)
		}
	}
	return queries, nil
}

func validateWebSearchStrings(values ...*string) error {
	for _, value := range values {
		if value != nil && *value == "" {
			return fmt.Errorf("web search detail contains an empty string")
		}
	}
	return nil
}
