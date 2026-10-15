import { create, validate } from "@app/server-api-contract";
import {
  GraphDraftSchema,
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
