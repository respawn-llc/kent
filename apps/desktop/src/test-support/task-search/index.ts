import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import type { FakeRoute } from "../api";
import { createTestServices } from "../app-services";
import { vi } from "vitest";

export function createSearchTestServices(routes: readonly FakeRoute[]) {
  const services = createTestServices(routes);
  return { ...services, searches: vi.spyOn(services.api, "searchTasks") };
}

export const searchResponse = create(pb.SearchSuccessSchema, {
  mode: pb.SearchMode.LITERAL,
  groups: [
    {
      projectId: "project-1",
      projectKey: "KNT",
      taskId: "task-1",
      shortId: "KNT-1",
      workflowId: "11111111-1111-4111-8111-111111111111",
      title: "Search the board",
      status: {
        kind: pb.TaskStatusKind.ACTIVE,
        nativeState: pb.TaskNativeState.ACTIVE,
        nodeIds: ["node-1"],
        attentionTypes: [],
      },
      totalHitCount: 4,
      hits: [
        {
          ordinal: 1,
          source: { kind: pb.SearchSourceKind.TITLE },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "Search",
              after: " the board",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
        {
          ordinal: 2,
          source: { kind: pb.SearchSourceKind.BODY },
          match: {
            case: "literal",
            value: {
              before: "Build ",
              match: "search",
              after: " UI",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
        {
          ordinal: 3,
          source: { kind: pb.SearchSourceKind.COMMENT, commentId: "comment-1" },
          match: {
            case: "literal",
            value: {
              before: "Please ",
              match: "search",
              after: " comments",
              leftTruncated: true,
              rightTruncated: true,
            },
          },
        },
      ],
    },
    {
      projectId: "project-1",
      projectKey: "KNT",
      taskId: "task-2",
      shortId: "KNT-2",
      workflowId: "22222222-2222-4222-8222-222222222222",
      title: "Second result",
      status: {
        kind: pb.TaskStatusKind.DONE,
        nativeState: pb.TaskNativeState.TERMINAL,
        nodeIds: [],
        attentionTypes: [],
      },
      totalHitCount: 1,
      hits: [
        {
          ordinal: 1,
          source: { kind: pb.SearchSourceKind.BODY },
          match: {
            case: "literal",
            value: {
              before: "Another ",
              match: "search",
              after: " result",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
      ],
    },
  ],
});

export const shortIDSearchResponse = create(pb.SearchSuccessSchema, {
  mode: pb.SearchMode.LITERAL,
  groups: [
    {
      projectId: "project-1",
      projectKey: "KNT",
      taskId: "task-exact",
      shortId: "KNT-345",
      workflowId: "11111111-1111-4111-8111-111111111111",
      title: "Exact identifier",
      status: {
        kind: pb.TaskStatusKind.ACTIVE,
        nativeState: pb.TaskNativeState.ACTIVE,
        nodeIds: ["node-1"],
        attentionTypes: [],
      },
      totalHitCount: 6,
      hits: [
        {
          ordinal: 1,
          source: { kind: pb.SearchSourceKind.SHORT_ID },
          match: {
            case: "literal",
            value: { before: "KNT-", match: "345", after: "", leftTruncated: false, rightTruncated: false },
          },
        },
        {
          ordinal: 2,
          source: { kind: pb.SearchSourceKind.TITLE },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "Preview two",
              after: "",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
        {
          ordinal: 3,
          source: { kind: pb.SearchSourceKind.BODY },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "Preview three",
              after: "",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
        {
          ordinal: 4,
          source: { kind: pb.SearchSourceKind.COMMENT, commentId: "comment-1" },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "Preview four",
              after: "",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
        {
          ordinal: 5,
          source: { kind: pb.SearchSourceKind.BODY },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "Preview five",
              after: "",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
      ],
    },
    {
      projectId: "project-1",
      projectKey: "KNT",
      taskId: "task-text",
      shortId: "KNT-999",
      workflowId: "11111111-1111-4111-8111-111111111111",
      title: "345 title match",
      status: {
        kind: pb.TaskStatusKind.BACKLOG,
        nativeState: pb.TaskNativeState.ACTIVE,
        nodeIds: [],
        attentionTypes: [],
      },
      totalHitCount: 1,
      hits: [
        {
          ordinal: 1,
          source: { kind: pb.SearchSourceKind.TITLE },
          match: {
            case: "literal",
            value: {
              before: "",
              match: "345",
              after: " title match",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        },
      ],
    },
  ],
});

export function searchRoute(
  result: (callIndex: number) => pb.SearchSuccess | Promise<pb.SearchSuccess>,
): FakeRoute {
  return {
    descriptor: pb.TaskReadService.method.search,
    resultFactory: async (_request, callIndex) =>
      create(pb.SearchResultSchema, { outcome: { case: "success", value: await result(callIndex) } }),
  };
}

export function shortIDContinuationFixture() {
  const group = shortIDSearchResponse.groups[0];
  if (group === undefined) throw new Error("Search Group fixture is required.");
  return create(pb.SearchSuccessSchema, {
    ...shortIDSearchResponse,
    groups: [create(pb.SearchGroupSchema, { ...group, hits: group.hits.slice(1) })],
  });
}
