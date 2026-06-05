import { describe, expect, it } from "vitest";

import { workflowPromptTemplatePlaceholders } from "./workflowPromptTemplatePlaceholders";

describe("workflowPromptTemplatePlaceholders", () => {
  it("orders dynamic parameter placeholders before muted built-in placeholders", () => {
    expect(
      workflowPromptTemplatePlaceholders([
        { key: "summary" },
        { key: "INVALID" },
        { key: "notes" },
        { key: "summary" },
      ]),
    ).toEqual([
      { label: ".Params.summary", tone: "primary", value: "{{.Params.summary}}" },
      { label: ".Params.notes", tone: "primary", value: "{{.Params.notes}}" },
      { label: ".TaskId", tone: "muted", value: "{{.TaskId}}" },
      { label: ".TaskShortId", tone: "muted", value: "{{.TaskShortId}}" },
      { label: ".TaskTitle", tone: "muted", value: "{{.TaskTitle}}" },
      { label: ".TaskBody", tone: "muted", value: "{{.TaskBody}}" },
      { label: ".NodeId", tone: "muted", value: "{{.NodeId}}" },
      { label: ".NodeKey", tone: "muted", value: "{{.NodeKey}}" },
      { label: ".NodeDisplayName", tone: "muted", value: "{{.NodeDisplayName}}" },
    ]);
  });
});
