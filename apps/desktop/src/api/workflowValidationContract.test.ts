import { expect, it } from "bun:test";
import { toBinary } from "@bufbuild/protobuf";
import { create, operationName } from "@app/server-api-contract";
import { ResultSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { completeDescriptorResponse } from "./descriptorRpc";
import { ContractError } from "./errors";

const workflowID = "11111111-1111-4111-8111-111111111111";

function clientWithErrors(errors: readonly wf.WorkflowValidationError[]) {
  const descriptor = wf.WorkflowDefinitionService.method.validate;
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor,
        result: create(descriptor.output, {
          outcome: { case: "success", value: { valid: false, errors: [...errors] } },
        }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

it("preserves validation codes, messages, and issue order", async () => {
  const errors = [
    create(wf.WorkflowValidationErrorSchema, {
      code: wf.ValidationErrorCode.SESSION_TRANSITION_AMBIGUOUS,
      message: "server diagnostic A",
      workflowId: workflowID,
      relatedIds: ["22222222-2222-4222-8222-222222222222"],
    }),
    create(wf.WorkflowValidationErrorSchema, {
      code: wf.ValidationErrorCode.SCRIPT_PATH_MISSING,
      message: "server diagnostic B",
      workflowId: workflowID,
      blocksContext: true,
    }),
  ];
  const result = await clientWithErrors(errors).validateWorkflow(workflowID, "execution");
  expect(result.errors).toMatchObject(
    errors.map((error) => ({
      code: error.code,
      message: error.message,
      workflowID: error.workflowId,
      relatedIDs: error.relatedIds,
      blocksContext: error.blocksContext,
    })),
  );
});

it.each([wf.ValidationErrorCode.UNSPECIFIED, 999999])(
  "rejects malformed validation code %s",
  (code: number) => {
    const error = create(wf.WorkflowValidationErrorSchema, {
      code,
      message: "server diagnostic",
      workflowId: workflowID,
    });
    const descriptor = wf.WorkflowDefinitionService.method.validate;
    const response = create(descriptor.output, {
      outcome: { case: "success", value: { valid: false, errors: [error] } },
    });
    // Serialize without output validation so rejection occurs on incoming bytes.
    const payload = toBinary(descriptor.output, response);
    expect(() =>
      completeDescriptorResponse(descriptor, "validation-response", {
        kind: "result",
        correlation: "validation-response",
        result: create(ResultSchema, { operation: operationName(descriptor), payload }),
      }),
    ).toThrow(ContractError);
  },
);
