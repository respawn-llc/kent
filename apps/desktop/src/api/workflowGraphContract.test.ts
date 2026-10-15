import { create, validate } from "@app/server-api-contract";
import {
  GraphDraftSchema,
  GraphSaveRequestSchema,
  GraphSavePreviewRequestSchema,
  GraphSaveSuccessSchema,
  ExecutionTargetMode,
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

describe("Workflow Save outcome blockers", () => {
  it("requires blockers exactly when not saved", () => {
    for (const saved of [false, true]) {
      for (const blocked of [false, true]) {
        const response = create(GraphSaveSuccessSchema, {
          saved,
          impact: {},
          definition: saved
            ? {
                workflow: {
                  id: "11111111-1111-4111-8111-111111111111",
                  name: "Workflow",
                  executionTargetPolicy: { mode: ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE },
                },
                derivedWiring: {},
              }
            : undefined,
          blockers: blocked ? [{ code: "version_changed", message: "Version changed" }] : [],
        });
        if (saved !== blocked) {
          expect(() => validate(GraphSaveSuccessSchema, response)).not.toThrow();
        } else {
          expect(() => validate(GraphSaveSuccessSchema, response)).toThrow();
        }
      }
    }
  });
});

describe("Workflow saved group identity", () => {
  it("requires normalized explicit node group membership only at Save and Preview", () => {
    for (const groupId of [undefined, "group", " group ", "missing", " "]) {
      const graph = create(GraphDraftSchema, {
        nodeGroups: [{ id: " group ", key: "key" }],
        nodes: [{ kind: NodeKind.WORKFLOW_NODE_KIND_START, groupId }],
      });
      expect(() => validate(GraphDraftSchema, graph)).not.toThrow();
      for (const schema of [GraphSaveRequestSchema, GraphSavePreviewRequestSchema]) {
        const request = create(schema, { workflowId: "11111111-1111-4111-8111-111111111111", graph });
        if (groupId === undefined || groupId === "group" || groupId === " group ") {
          expect(() => validate(schema, request)).not.toThrow();
        } else {
          expect(() => validate(schema, request)).toThrow();
        }
      }
    }
  });
  it("requires normalized IDs and keys to be unique only at Save and Preview", () => {
    for (const { nodeGroups, valid } of [
      {
        nodeGroups: [
          { id: " a ", key: " x " },
          { id: "b", key: "y" },
        ],
        valid: true,
      },
      {
        nodeGroups: [
          { id: " a ", key: "x" },
          { id: "a", key: "y" },
        ],
        valid: false,
      },
      {
        nodeGroups: [
          { id: "a", key: " x " },
          { id: "b", key: "x" },
        ],
        valid: false,
      },
    ]) {
      const graph = create(GraphDraftSchema, { nodeGroups });
      expect(() => validate(GraphDraftSchema, graph)).not.toThrow();
      for (const schema of [GraphSaveRequestSchema, GraphSavePreviewRequestSchema]) {
        const request = create(schema, { workflowId: "11111111-1111-4111-8111-111111111111", graph });
        if (valid) {
          expect(() => validate(schema, request)).not.toThrow();
        } else {
          expect(() => validate(schema, request)).toThrow();
        }
      }
    }
  });
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
