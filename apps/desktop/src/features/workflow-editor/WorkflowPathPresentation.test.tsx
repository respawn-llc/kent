import { render, screen } from "@testing-library/react";
import { beforeAll, expect, it } from "vitest";

import { emptyWorkflowDerivedWiring, type WorkflowDefinition, type WorkflowNode } from "@/api";
import { initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { NodeDetails } from "./WorkflowReadonlyInspector";

beforeAll(initializeI18n);

it("collapses a read-only Script path without changing its canonical value", () => {
  const node: WorkflowNode = {
    id: "script",
    workflowID: "workflow",
    key: "script",
    kind: "script",
    name: "Script",
    groupID: null,
    groupKey: "",
    subagentRole: "",
    joinInputProviders: [],
    scriptPath: "/Users/engineer/project/build.sh",
  };
  const definition: WorkflowDefinition = {
    workflow: {
      id: "workflow",
      name: "Workflow",
      description: "",
      version: 1,
      executionTargetPolicy: { customRef: null, mode: "none" },
    },
    nodes: [node],
    edges: [],
    nodeGroups: [],
    transitionGroups: [],
    derivedWiring: emptyWorkflowDerivedWiring,
  };
  render(
    <TestAppProviders
      services={createTestServices([], undefined, { homePath: "/Users/engineer", platform: "macos" })}
    >
      <NodeDetails definition={definition} node={node} validation={{ valid: true, errors: [] }} />
    </TestAppProviders>,
  );
  expect(screen.getByText("~/project/build.sh")).toBeInTheDocument();
  expect(node.scriptPath).toBe("/Users/engineer/project/build.sh");
});
