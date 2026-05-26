import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { ValidationDetails } from "./WorkflowInspectorPrimitives";

void initializeI18n();

describe("ValidationDetails", () => {
  it("shows structured validation details in inspector cards", () => {
    render(
      <ValidationDetails
        errors={[
          {
            blocksContext: true,
            code: "workflow.validation.invalid_join_input_provider",
            details: {
              fieldName: "",
              inputName: "summary",
              placeholder: "",
              providerEdgeID: "edge-provider",
            },
            edgeID: "edge-provider",
            message: "Join input provider is invalid.",
            nodeID: "join",
            relatedIDs: [],
            transitionGroupID: "",
            workflowID: "workflow-1",
          },
        ]}
      />,
    );

    expect(screen.getAllByRole("listitem")).toHaveLength(1);
  });
});
