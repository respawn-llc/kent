package runtimeview

import (
	"fmt"
	"slices"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/transcript"
)

func webSearchDetailToProto(detail *transcript.WebSearchDetail) (*transcriptpb.WebSearchDetail, error) {
	if detail == nil {
		return nil, nil
	}
	out := &transcriptpb.WebSearchDetail{Sources: slices.Clone(detail.Sources)}
	switch action := detail.Action.(type) {
	case transcript.WebSearchSearch:
		out.Action = &transcriptpb.WebSearchDetail_Search{Search: &transcriptpb.WebSearchSearch{Queries: slices.Clone(action.Queries)}}
	case transcript.WebSearchOpenPage:
		out.Action = &transcriptpb.WebSearchDetail_OpenPage{OpenPage: &transcriptpb.WebSearchOpenPage{Url: textutil.Pointer(action.URL)}}
	case transcript.WebSearchFindInPage:
		out.Action = &transcriptpb.WebSearchDetail_FindInPage{FindInPage: &transcriptpb.WebSearchFindInPage{Url: textutil.Pointer(action.URL), Pattern: textutil.Pointer(action.Pattern)}}
	default:
		return nil, fmt.Errorf("unsupported web search action %T", action)
	}
	for _, result := range detail.Results {
		kind := transcriptpb.WebSearchResultKind_WEB_SEARCH_RESULT_KIND_LINK
		if result.Image {
			kind = transcriptpb.WebSearchResultKind_WEB_SEARCH_RESULT_KIND_IMAGE
		}
		out.Results = append(out.Results, &transcriptpb.WebSearchResult{
			Kind: kind, Title: textutil.Pointer(result.Title), Destination: textutil.Pointer(result.Destination),
		})
	}
	return out, nil
}
