import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { create } from "@app/server-api-contract";
import { unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { FakeRpcTransport } from "@/test-support/api";
describe("ApiClient workflow script path validation", () => {
  it("maps workflow script path validation", async () => {
    const nodeID = "10000000-0000-4000-8000-000000000005";
    const transport = new FakeRpcTransport([
      {
        descriptor: wf.WorkflowDefinitionService.method.validateScriptPath,
        result: create(wf.WorkflowDefinitionService.method.validateScriptPath.output, {
          outcome: {
            case: "success",
            value: {
              valid: false,
              errors: [
                {
                  code: wf.ValidationErrorCode.SCRIPT_PATH_MISSING,
                  message: "script_path is required",
                  workflowId: "11111111-1111-4111-8111-111111111111",
                  nodeId: nodeID,
                  transitionGroupId: undefined,
                  edgeId: undefined,
                  details: {
                    providerEdgeId: undefined,
                  },
                  blocksContext: true,
                },
              ],
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(
      client.validateWorkflowScriptPath({
        workflowID: "11111111-1111-4111-8111-111111111111",
        nodeID,
        scriptPath: "scripts/run",
      }),
    ).resolves.toMatchObject({ valid: false });
    expect(transport.descriptorCalls).toContainEqual({
      descriptor: wf.WorkflowDefinitionService.method.validateScriptPath,
      request: create(wf.WorkflowDefinitionService.method.validateScriptPath.input, {
        workflowId: "11111111-1111-4111-8111-111111111111",
        nodeId: nodeID,
        scriptPath: "scripts/run",
      }),
    });
  });
});
