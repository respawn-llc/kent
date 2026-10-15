import { create, validate } from "@app/server-api-contract";
import {
  GraphDraftSchema,
  GraphSaveRequestSchema,
  GraphSavePreviewRequestSchema,
  NodeKind,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";

describe("Workflow draft group-key resolution", () => {
  it("requires normalized key membership unless an explicit ID overrides the key", () => {
    for (const { groupKey, groupId, valid } of [
      { groupKey: "", valid: true },
      { groupKey: " \u0085", valid: true },
      { groupKey: " group ", valid: true },
      { groupKey: "missing", valid: false },
      { groupKey: "missing", groupId: "explicit", valid: true },
      { groupKey: "missing", groupId: " ", valid: true },
    ]) {
      const graph = create(GraphDraftSchema, {
        nodeGroups: [{ id: "", key: " group " }],
        nodes: [{ kind: NodeKind.WORKFLOW_NODE_KIND_START, groupKey, groupId }],
      });
      if (valid) {
        expect(() => validate(GraphDraftSchema, graph)).not.toThrow();
      } else {
        expect(() => validate(GraphDraftSchema, graph)).toThrow();
      }
    }
  });
});

describe("Workflow saved group identity", () => {
  it("requires substantive normalized IDs and keys only at Save and Preview", () => {
    for (const { id, key, valid } of [
      { id: " group-id ", key: " group-key ", valid: true },
      { id: "\uFEFF", key: "\uFEFF", valid: true },
      { id: " \u0085", key: "key", valid: false },
      { id: "id", key: " \t", valid: false },
    ]) {
      const graph = create(GraphDraftSchema, { nodeGroups: [{ id, key }] });
      expect(() => validate(GraphDraftSchema, graph)).not.toThrow();
      for (const schema of [GraphSaveRequestSchema, GraphSavePreviewRequestSchema]) {
        const request = create(schema, {
          workflowId: "11111111-1111-4111-8111-111111111111",
          graph,
        });
        if (valid) {
          expect(() => validate(schema, request)).not.toThrow();
        } else {
          expect(() => validate(schema, request)).toThrow();
        }
      }
    }
  });
});
