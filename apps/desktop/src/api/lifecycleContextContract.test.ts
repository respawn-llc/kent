import { create, validate } from "@app/server-api-contract";
import * as P from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import * as R from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";

describe("Lifecycle context and prompt history contract", () => {
  const sessionId = "11111111-1111-4111-8111-111111111111";

  it("accepts absent names and rejects present blank names", () => {
    for (const sessionName of [undefined, " \t", "session"]) {
      const message = create(T.SessionIdentitySchema, {
        sessionId,
        sessionName,
        conversationFreshness: R.ConversationFreshness.FRESH,
      });
      if (sessionName === " \t") {
        expect(() => validate(T.SessionIdentitySchema, message)).toThrow();
      } else {
        expect(() => validate(T.SessionIdentitySchema, message)).not.toThrow();
      }
    }
  });

  it("requires substantive Workflow Task identity only when present", () => {
    for (const taskId of [undefined, " \t", "task-1"]) {
      const message = create(T.SessionStatusSchema, {
        reviewerFrequency: "off",
        thinkingLevel: "medium",
        compactionMode: "none",
        workflow: taskId === undefined ? undefined : { taskId, workflowId: "workflow-1" },
      });
      if (taskId === " \t") {
        expect(() => validate(T.SessionStatusSchema, message)).toThrow();
      } else {
        expect(() => validate(T.SessionStatusSchema, message)).not.toThrow();
      }
    }
  });

  it("preserves Go whitespace acceptance for Prompt History", () => {
    for (const text of ["", " \t\n", "\u0085", "\uFEFF", "  prompt\n"]) {
      const message = create(P.RecordHistoryRequestSchema, { sessionId, text });
      if (["\uFEFF", "  prompt\n"].includes(text)) {
        expect(() => validate(P.RecordHistoryRequestSchema, message)).not.toThrow();
      } else {
        expect(() => validate(P.RecordHistoryRequestSchema, message)).toThrow();
      }
    }
  });
});
