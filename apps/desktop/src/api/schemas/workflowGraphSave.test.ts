import { expect, it } from "vitest";
import { create, validate } from "@app/server-api-contract";
import {
  GraphEntityType,
  GraphSavePreviewSuccessSchema,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { workflowSavePreview } from "../clientWorkflowProjection";

const edgeID = "40000000-0000-4000-8000-000000000001";

it("preserves graph impact and blocker identities through generated validation", () => {
  const reference = { entityType: GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_EDGE, entityId: edgeID };
  const preview = create(GraphSavePreviewSuccessSchema, {
    changed: true,
    currentVersion: 12n,
    confirmationRequired: true,
    impact: { removedEdgeCount: 1n, removedEntities: [reference] },
    blockers: [
      {
        code: "confirmation_required",
        count: 1n,
        message: "Confirm removal.",
        affectedEntities: [reference],
      },
    ],
  });
  validate(GraphSavePreviewSuccessSchema, preview);
  expect(workflowSavePreview(preview)).toMatchObject({
    changed: true,
    impact: { removedEntities: [{ entityID: edgeID, entityType: "edge" }] },
    blockers: [{ affectedEntities: [{ entityID: edgeID, entityType: "edge" }] }],
  });
});
