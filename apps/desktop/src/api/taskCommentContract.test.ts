import { create, validate } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

describe("Task comment body contract", () => {
  it("requires substantive bodies for Add and Replace using Go whitespace rules", () => {
    for (const body of [" \t\n", "\u0085", "  comment\n", "\uFEFF"]) {
      const add = create(T.CommentAddRequestSchema, {
        taskId: "task-1",
        body,
        author: T.CommentAuthorKind.USER,
      });
      const replace = create(T.CommentReplaceRequestSchema, { commentId: "comment-1", body });
      if (body === "  comment\n" || body === "\uFEFF") {
        expect(() => validate(T.CommentAddRequestSchema, add)).not.toThrow();
        expect(() => validate(T.CommentReplaceRequestSchema, replace)).not.toThrow();
      } else {
        expect(() => validate(T.CommentAddRequestSchema, add)).toThrow();
        expect(() => validate(T.CommentReplaceRequestSchema, replace)).toThrow();
      }
    }
  });
});
