package sessionnaming

import (
	"strings"
	"testing"
	"unicode/utf8"

	"core/server/llm"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestNamingRequestBudget(t *testing.T) {
	target := llm.NanoTarget{Settings: config.Settings{Model: "local", ModelContextWindow: 2500}, Estimator: llm.DefaultTokenEstimator{}}
	task := "先" + strings.Repeat("請求金額を修正する", 6000) + "後"
	commentary := "始" + strings.Repeat("計算の問題を調査中", 6000) + "終"
	request, err := PrepareRequest(target, task, &commentary)
	if err != nil {
		t.Fatal(err)
	}
	input, err := llm.EstimateRequestTokens(target.Estimator, request)
	if err != nil {
		t.Fatal(err)
	}
	if input > 2500*85/100 || input+request.MaxTokens > 2500 || len(request.Tools) != 0 || request.StructuredOutput != nil {
		t.Fatalf("naming budget/features = %d/%d, %+v", input, request.MaxTokens, request)
	}
	if len(request.Items) != 1 || request.Items[0].Content == nil || !utf8.ValidString(*request.Items[0].Content) {
		t.Fatal("invalid naming message")
	}
	for _, endpoint := range []string{"先", "後", "始", "終"} {
		if !strings.Contains(*request.Items[0].Content, endpoint) {
			t.Fatalf("excerpt lost endpoint %q", endpoint)
		}
	}
}

func TestNamingOptionalCommentaryAndMinimalFailure(t *testing.T) {
	target := llm.NanoTarget{Settings: config.Settings{Model: "local", ModelContextWindow: 2500}, Estimator: llm.DefaultTokenEstimator{}}
	request, err := PrepareRequest(target, "original task", nil)
	if err != nil || len(request.Items) != 1 || request.Items[0].Content == nil {
		t.Fatalf("optional commentary request = %+v, %v", request, err)
	}
	target.Settings.ModelContextWindow = 1
	if _, err := PrepareRequest(target, "task", nil); err == nil {
		t.Fatal("impossible minimal prompt accepted")
	}
}

func TestNamingTitleInterpretation(t *testing.T) {
	shortID, err := runtimeids.NewSessionShortID(7)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", " \n\t", "First\nSecond", "First\rSecond"} {
		if _, err := InterpretTitle(value, sessioncontract.SessionCategoryMain, shortID); err == nil {
			t.Fatalf("invalid title accepted: %q", value)
		}
	}
	title, err := InterpretTitle("  Fix totals  ", sessioncontract.SessionCategorySubagent, shortID)
	if err != nil || title != "Subagent 7: Fix totals" {
		t.Fatalf("subagent title = %q, %v", title, err)
	}
	long := strings.Repeat("word ", 10)
	title, err = InterpretTitle(long, sessioncontract.SessionCategoryMain, shortID)
	if err != nil || title != strings.TrimSpace(long) {
		t.Fatal("prompt style was enforced by rewriting the result")
	}
	if excerpt := textutil.MiddleTruncate("始abcdef終", 5); excerpt != "始a…f終" {
		t.Fatalf("middle excerpt = %q", excerpt)
	}
}
