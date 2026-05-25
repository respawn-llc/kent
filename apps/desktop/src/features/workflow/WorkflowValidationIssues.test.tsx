import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { WorkflowValidationIssues } from "./WorkflowValidationIssues";

void initializeI18n();

describe("WorkflowValidationIssues", () => {
  it("renders structured validation details next to the server message", () => {
    render(
      <WorkflowValidationIssues
        errors={[
          {
            blocksContext: true,
            code: "workflow.validation.invalid_template_placeholder",
            details: {
              fieldName: "summary",
              inputName: "summary",
              placeholder: ".Inputs.summary",
              providerEdgeID: "edge-provider",
            },
            edgeID: "",
            message: "Prompt template references an unknown node input.",
            nodeID: "node-1",
            relatedIDs: [],
            transitionGroupID: "",
            workflowID: "workflow-1",
          },
        ]}
      />,
    );

    expect(screen.getByText("Prompt template references an unknown node input.")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Input: summary · Field: summary · Placeholder: .Inputs.summary · Provider edge: edge-provider",
      ),
    ).toBeInTheDocument();
  });
});
