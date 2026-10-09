import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { RpcError, TaskSearchError, type TaskSearchInput } from "./index";

const method = pb.TaskReadService.method.search;
const workflowID = "11111111-1111-4111-8111-111111111111";
const firstHit = create(pb.SearchHitSchema, {
  ordinal: 1,
  source: { kind: pb.SearchSourceKind.TITLE },
  match: { case: "literal", value: { match: "Search", after: " tasks" } },
});
const secondHit = create(pb.SearchHitSchema, {
  ordinal: 2,
  source: { kind: pb.SearchSourceKind.COMMENT, commentId: "comment-1" },
  match: {
    case: "literal",
    value: {
      before: "Please ",
      match: "search",
      after: " this",
      leftTruncated: true,
      rightTruncated: true,
    },
  },
});
const group = create(pb.SearchGroupSchema, {
  projectId: "project-1",
  projectKey: "KNT",
  taskId: "task-1",
  shortId: "KNT-1",
  workflowId: workflowID,
  title: "Search tasks",
  status: { kind: pb.TaskStatusKind.ACTIVE, nativeState: pb.TaskNativeState.ACTIVE, nodeIds: ["node-1"] },
  totalHitCount: 2,
  hits: [firstHit, secondHit],
});
const response = create(pb.SearchSuccessSchema, {
  mode: pb.SearchMode.LITERAL,
  groups: [group],
  nextOffset: 25,
});
const input: TaskSearchInput = {
  mode: "literal",
  query: "search",
  context: 20,
  caseSensitive: false,
  includeComments: true,
  projectIDs: ["project-1", "project-2"],
  pageSize: 25,
  offset: 5,
};

function searchClient(result: pb.SearchResult) {
  const transport = new FakeRpcTransport([{ descriptor: method, result }]);
  return { client: new ApiClient(transport, unexpectedProjectOverflow), transport };
}

function success(value: pb.SearchSuccess) {
  return create(method.output, { outcome: { case: "success", value } });
}

describe("ApiClient task search", () => {
  it("sends literal filters and maps grouped content without changing text", async () => {
    const { client, transport } = searchClient(success(response));
    const controller = new AbortController();
    await expect(client.searchTasks(input, controller.signal)).resolves.toEqual({
      mode: "literal",
      nextOffset: 25,
      groups: [
        {
          projectID: "project-1",
          projectKey: "KNT",
          taskID: "task-1",
          shortID: "KNT-1",
          workflowID,
          title: "Search tasks",
          status: { kind: "active", nativeState: "active", nodeIDs: ["node-1"], attentionTypes: [] },
          totalHitCount: 2,
          hits: [
            {
              ordinal: 1,
              source: { kind: "title" },
              literal: {
                before: "",
                match: "Search",
                after: " tasks",
                leftTruncated: false,
                rightTruncated: false,
              },
            },
            {
              ordinal: 2,
              source: { kind: "comment", commentID: "comment-1" },
              literal: {
                before: "Please ",
                match: "search",
                after: " this",
                leftTruncated: true,
                rightTruncated: true,
              },
            },
          ],
        },
      ],
    });
    expect(transport.descriptorCalls).toEqual([
      {
        descriptor: method,
        request: create(method.input, {
          mode: pb.SearchMode.LITERAL,
          query: input.query,
          context: input.context,
          includeComments: true,
          projectIds: ["project-1", "project-2"],
          pageSize: input.pageSize,
          offset: input.offset,
        }),
        options: { signal: controller.signal },
      },
    ]);
  });

  it("maps an absent continuation to absent pagination", async () => {
    const { client } = searchClient(
      success(create(pb.SearchSuccessSchema, { ...response, nextOffset: undefined })),
    );
    await expect(client.searchTasks(input)).resolves.toMatchObject({ mode: "literal", nextOffset: null });
  });

  it("decodes a literal Short ID hit", async () => {
    const hit = create(pb.SearchHitSchema, {
      ordinal: 1,
      source: { kind: pb.SearchSourceKind.SHORT_ID },
      match: { case: "literal", value: { before: "KNT-", match: "345" } },
    });
    const { client } = searchClient(
      success(
        create(pb.SearchSuccessSchema, {
          ...response,
          groups: [create(pb.SearchGroupSchema, { ...group, totalHitCount: 1, hits: [hit] })],
        }),
      ),
    );
    await expect(client.searchTasks(input)).resolves.toMatchObject({
      groups: [{ hits: [{ source: { kind: "short_id" }, literal: { match: "345" } }] }],
    });
  });

  it("rejects a raw FTS5 Short ID hit", async () => {
    const hit = create(pb.SearchHitSchema, {
      ordinal: 1,
      source: { kind: pb.SearchSourceKind.SHORT_ID },
      match: { case: "fts5", value: { snippet: "345" } },
    });
    const { client } = searchClient(
      success(
        create(pb.SearchSuccessSchema, {
          ...response,
          mode: pb.SearchMode.FTS5,
          groups: [create(pb.SearchGroupSchema, { ...group, totalHitCount: 1, hits: [hit] })],
        }),
      ),
    );
    await expect(client.searchTasks({ ...input, mode: "fts5" })).rejects.toThrow();
  });

  it.each([
    {
      name: "a mode-incompatible hit payload",
      group: create(pb.SearchGroupSchema, {
        ...group,
        hits: [
          create(pb.SearchHitSchema, {
            ordinal: 1,
            source: { kind: pb.SearchSourceKind.TITLE },
            match: { case: "fts5", value: { snippet: "search" } },
          }),
        ],
      }),
    },
    {
      name: "a non-canonical task status",
      group: create(pb.SearchGroupSchema, {
        ...group,
        status: create(pb.TaskStatusSchema, {
          kind: pb.TaskStatusKind.ACTIVE,
          nativeState: pb.TaskNativeState.RUNNING,
        }),
      }),
    },
    {
      name: "unordered hit ordinals",
      group: create(pb.SearchGroupSchema, { ...group, hits: [secondHit, firstHit] }),
    },
  ])("rejects $name", async ({ group }) => {
    const { client } = searchClient(
      success(create(pb.SearchSuccessSchema, { ...response, groups: [group] })),
    );
    await expect(client.searchTasks(input)).rejects.toThrow();
  });

  it("surfaces normalized-too-short as a typed outcome", async () => {
    const { client } = searchClient(
      create(method.output, {
        outcome: {
          case: "error",
          value: {
            code: "normalized_too_short",
            detail: { case: "normalizedTooShort", value: {} },
          },
        },
      }),
    );
    const error: unknown = await client
      .searchTasks({ ...input, query: "ab" })
      .catch((error: unknown) => error);
    expect(error).toBeInstanceOf(TaskSearchError);
    expect(error).toMatchObject({ reason: "normalized_too_short" });
  });

  it("keeps unknown error codes generic even with a known detail", async () => {
    const { client } = searchClient(
      create(method.output, {
        outcome: {
          case: "error",
          value: {
            code: "future_search_failure",
            detail: { case: "normalizedTooShort", value: {} },
          },
        },
      }),
    );
    const error: unknown = await client.searchTasks(input).catch((error: unknown) => error);
    expect(error).toBeInstanceOf(RpcError);
    expect(error).not.toBeInstanceOf(TaskSearchError);
  });
});
