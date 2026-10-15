import { create, validate } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import * as S from "@app/server-api-contract/gen/kent/api/session/session_pb";

describe("Optional Session display number", () => {
  it("requires a UUID while allowing neither, UUID alone and both", () => {
    const id = "11111111-1111-4111-8111-111111111111";
    for (const pair of [
      { sessionId: undefined, shortId: undefined },
      { sessionId: id, shortId: undefined },
      { sessionId: id, shortId: 1n },
      { sessionId: undefined, shortId: 1n },
    ]) {
      const question = create(T.ObserveQuestionSchema, {
        ...pair,
        question: {
          question: {
            case: "ask",
            value: {
              sessionId: id,
              stepId: id,
              toolCallId: "call-1",
              question: "Continue?",
              createdAt: { seconds: 1n, nanos: 0 },
            },
          },
        },
      });
      const failure = create(T.ObserveFailureSchema, { ...pair, failure: { reason: "failed" } });
      const history = create(S.QuestionHistoryQuestionSchema, {
        answeredBySessionId: pair.sessionId,
        answeredBySessionShortId: pair.shortId,
        question: "Continue?",
        answer: "Yes",
      });
      if (pair.sessionId === undefined && pair.shortId !== undefined) {
        expect(() => validate(T.ObserveQuestionSchema, question)).toThrow();
        expect(() => validate(T.ObserveFailureSchema, failure)).toThrow();
        expect(() => validate(S.QuestionHistoryQuestionSchema, history)).toThrow();
      } else {
        expect(() => validate(T.ObserveQuestionSchema, question)).not.toThrow();
        expect(() => validate(T.ObserveFailureSchema, failure)).not.toThrow();
        expect(() => validate(S.QuestionHistoryQuestionSchema, history)).not.toThrow();
      }
    }
  });
});
