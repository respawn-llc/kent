import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { initializeI18n } from "../../i18n/setup";
import { DetailSection, ValidationDetails } from "./WorkflowInspectorPrimitives";

void initializeI18n();

describe("ValidationDetails", () => {
  it("exposes titled inspector sections as named regions", () => {
    render(
      <DetailSection title="Route">
        <button type="button">Target node</button>
      </DetailSection>,
    );

    const routeSection = screen.getByRole("region", { name: "Route" });

    expect(within(routeSection).getByRole("heading", { level: 3, name: "Route" })).toBeInTheDocument();
    expect(within(routeSection).getByRole("button", { name: "Target node" })).toBeInTheDocument();
  });

  it("can preserve region naming while visually hiding a section title", () => {
    render(
      <DetailSection hideTitle title="Route">
        <button type="button">Context mode</button>
      </DetailSection>,
    );

    const routeSection = screen.getByRole("region", { name: "Route" });
    const hiddenHeading = within(routeSection).getByRole("heading", { level: 3, name: "Route" });

    expect(hiddenHeading).toHaveClass("sr-only");
    expect(within(routeSection).queryByText("Route", { selector: "h3:not(.sr-only)" })).not.toBeInTheDocument();
    expect(within(routeSection).getByRole("button", { name: "Context mode" })).toBeInTheDocument();
  });

  it("shows structured validation details as a plain bullet list", () => {
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

    const section = screen.getByRole("region", { name: "Validation errors" });
    const list = within(section).getByRole("list");
    const item = within(list).getByRole("listitem");

    expect(list).toHaveClass("list-disc");
    expect(item).not.toHaveClass("rounded-[var(--radius-m)]", "border");
    expect(within(item).getByText("Join input provider is invalid.")).toBeInTheDocument();
    expect(within(item).getByText("Input: summary · Provider edge: edge-provider")).toBeInTheDocument();
    expect(screen.queryByText("workflow.validation.invalid_join_input_provider")).not.toBeInTheDocument();
  });
});
