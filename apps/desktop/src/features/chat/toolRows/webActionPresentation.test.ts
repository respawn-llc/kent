import { expect, it, vi } from "bun:test";
import { row } from "@/test-support/transcript-window";
import { resolveToolPresentation, type ToolPresentationStrings } from "./toolPresentation";
import type { TranscriptCommittedToolRow, TranscriptToolSlotItem } from "./toolSlotTypes";

function fixture(action: NonNullable<TranscriptCommittedToolRow["Tool"]["WebSearch"]>["action"]) {
  const item: TranscriptToolSlotItem = {
    kind: "committed",
    row: {
      ...row(1),
      Kind: "tool",
      User: null,
      Tool: {
        ToolName: "web_search",
        ToolCallID: "web-activity",
        Text: "",
        IsError: false,
        ViewedImageKind: null,
        AnsweredBySessionID: null,
        AnsweredBySessionShortID: null,
        WebSearch: { action, results: [], sources: [] },
      },
    },
  };
  const strings = {
    backgrounded: "backgrounded",
    editFailed: "edit-failed",
    exitCode: String,
    moreLines: String,
    patchFailed: "patch-failed",
    searchedWeb: vi.fn((query: string) => query),
    openedWebPage: vi.fn((url: string) => url),
    foundInWebPage: vi.fn((url: string, pattern: string) => JSON.stringify({ url, pattern })),
    webActivity: "web",
    viewedImage: (path: string) => path,
    formatPath: (path: string) => path,
  };
  return { item, strings };
}

function webPresentation(item: TranscriptToolSlotItem, strings: ToolPresentationStrings) {
  const presentation = resolveToolPresentation(item, strings);
  if (presentation.kind !== "web-search") throw new Error("Expected web activity presentation");
  return presentation;
}

it("labels a page opening with its actual destination rather than a search query", () => {
  const url = "https://github.com/respawn-llc/kent/pull/1023";
  const { item, strings } = fixture({ kind: "open-page", url });
  const result = webPresentation(item, strings);
  expect(result.compact).toBe(url);
  expect(strings.searchedWeb).not.toHaveBeenCalled();
});

it("uses the completed search queries even when the start had no query metadata", () => {
  const queries = ["native recording timeline", "AVAssetWriter timestamps"];
  const { item, strings } = fixture({ kind: "search", queries });
  const result = webPresentation(item, strings);
  expect(result.compact).toBe(queries.join("; "));
});

it("keeps page text matching distinct from a web query", () => {
  const url = "https://example.com/reference";
  const pattern = "sample timestamps";
  const { item, strings } = fixture({ kind: "find-in-page", url, pattern });
  const result = webPresentation(item, strings);
  if (result.compact === null) throw new Error("Missing web activity summary");
  expect(JSON.parse(result.compact)).toEqual({ url, pattern });
  expect(strings.searchedWeb).not.toHaveBeenCalled();
});

it("does not claim a query while a native web action is still unknown", () => {
  const { strings } = fixture({ kind: "search", queries: [] });
  const result = webPresentation(
    {
      kind: "call",
      activity: "active",
      tool: { StepID: "step", ToolCallID: "web-activity", ToolName: "web_search" },
    },
    strings,
  );
  expect(result.compact).toBe(strings.webActivity);
  expect(strings.searchedWeb).not.toHaveBeenCalled();
});
