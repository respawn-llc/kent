import { create, operationName } from "@app/server-api-contract";
import { QuestionService } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { FakeRpcTransport } from "@/test-support/api";
import { ApiClient } from "./client";

it("attaches the Session for Session-scoped pending prompt reads", async () => {
  const transport = new FakeRpcTransport([
    {
      descriptor: QuestionService.method.listPending,
      result: create(QuestionService.method.listPending.output, {
        outcome: { case: "success", value: { questions: [] } },
      }),
    },
  ]);
  const client = new ApiClient(transport);

  await expect(client.listPendingAsks("session-1")).resolves.toEqual([]);

  expect(transport.calls).toEqual([]);
  expect(transport.attachedSessionCalls).toEqual([
    {
      sessionID: "session-1",
      method: operationName(QuestionService.method.listPending),
    },
  ]);
});

it("preserves pending-ask recommendation presence and rejects invalid indexes", async () => {
  const method = QuestionService.method.listPending;
  for (const recommendedOptionIndex of [undefined, 2, 3]) {
    const transport = new FakeRpcTransport([
      {
        descriptor: method,
        result: create(method.output, {
          outcome: {
            case: "success",
            value: {
              questions: [
                {
                  toolCallId: "ask-1",
                  sessionId: "session-1",
                  stepId: "11111111-1111-4111-8111-111111111111",
                  question: "Choose?",
                  suggestions: ["one", "two"],
                  createdAt: { seconds: 1785715200n, nanos: 0 },
                  recommendedOptionIndex,
                },
              ],
            },
          },
        }),
      },
    ]);
    const result = new ApiClient(transport).listPendingAsks("session-1");
    if (recommendedOptionIndex === 3) {
      await expect(result).rejects.toThrow();
    } else {
      await expect(result).resolves.toMatchObject([
        { recommendedOptionIndex: recommendedOptionIndex ?? null },
      ]);
    }
  }
});
