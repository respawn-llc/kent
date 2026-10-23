import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import {
  SessionTransitionAction,
  SessionTransitionSchema,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

describe("Session transition recorded prompt", () => {
  it("requires text only when the action consumes the recorded prompt", () => {
    for (const action of [
      SessionTransitionAction.NEW_SESSION,
      SessionTransitionAction.FORK_ROLLBACK,
      SessionTransitionAction.RESUME,
    ]) {
      for (const recorded of [false, true]) {
        for (const text of ["", " \t ", "prompt"]) {
          const request = create(SessionTransitionSchema, {
            action,
            initialPrompt: text,
            initialPromptHistoryRecorded: recorded,
            forkRollbackTargetId: action === SessionTransitionAction.FORK_ROLLBACK ? "target" : undefined,
          });
          const valid = action === SessionTransitionAction.RESUME || !recorded || text === "prompt";
          if (valid) {
            expect(() => validate(SessionTransitionSchema, request)).not.toThrow();
          } else {
            expect(() => validate(SessionTransitionSchema, request)).toThrow();
          }
        }
      }
    }
  });
});
