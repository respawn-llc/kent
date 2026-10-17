import { expect, it } from "bun:test";
import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { create } from "@app/server-api-contract";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import {
  workflowBoundaryGraphIDs,
  workflowGraphDraft,
  workflowDefinitionResponse,
  workflowGraphSaveImpactResponse,
} from "./clientWorkflowGraph.testFixtures";

it("preserves absent Group names across graph save and reload", async () => {
  const definition = {
    ...workflowDefinitionResponse.definition,
    nodeGroups: workflowDefinitionResponse.definition.nodeGroups.map((group) => ({
      ...group,
      displayName: undefined,
    })),
  };
  const method = wf.WorkflowGraphService.method.save;
  const transport = new FakeRpcTransport([
    {
      descriptor: method,
      result: create(method.output, {
        outcome: {
          case: "success",
          value: {
            saved: true,
            changed: true,
            currentVersion: 9n,
            definition,
            impact: workflowGraphSaveImpactResponse,
          },
        },
      }),
    },
    {
      descriptor: wf.WorkflowDefinitionService.method.get,
      result: create(wf.WorkflowDefinitionService.method.get.output, {
        outcome: { case: "success", value: { definition } },
      }),
    },
  ]);
  const client = new ApiClient(transport, unexpectedProjectOverflow);
  const saved = await client.saveWorkflowGraph({
    workflowID: definition.workflow.id,
    expectedVersion: 8,
    graph: {
      ...workflowGraphDraft,
      nodeGroups: [{ id: workflowBoundaryGraphIDs.nodeGroup, key: "core", name: null }],
    },
  });
  expect(saved.definition?.nodeGroups[0]?.name).toBeNull();
  expect(transport.descriptorCalls[0]?.request).not.toHaveProperty("graph.nodeGroups.0.displayName");
  const reloaded = await client.getWorkflow(definition.workflow.id);
  expect(reloaded.nodeGroups[0]?.name).toBeNull();
});
