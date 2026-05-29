import { describe, expect, it } from "vitest";

import { workflowPromptTemplatePlaceholders } from "./workflowPromptTemplatePlaceholders";

describe("workflowPromptTemplatePlaceholders", () => {
  it("orders dynamic input placeholders before muted built-in placeholders", () => {
    expect(
      workflowPromptTemplatePlaceholders([
        { name: "summary" },
        { name: "INVALID" },
        { name: "notes" },
        { name: "summary" },
      ]),
    ).toEqual([
      { label: ".Inputs.summary", tone: "primary", value: "{{.Inputs.summary}}" },
      { label: ".Inputs.notes", tone: "primary", value: "{{.Inputs.notes}}" },
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
