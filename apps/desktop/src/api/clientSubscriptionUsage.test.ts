import { expect, test } from "bun:test";
import { create } from "@app/server-api-contract";
import {
  AuthService,
  AuthMethod,
  SubscriptionWindowBucket,
  SubscriptionQuotaSource,
  SubscriptionFactsSchema,
  type SubscriptionFacts,
} from "@app/server-api-contract/gen/kent/api/auth/auth_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";

function client(method: AuthMethod, partialFailure?: string, subscription?: SubscriptionFacts) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: AuthService.method.getStatus,
        result: create(AuthService.method.getStatus.output, {
          outcome: {
            case: "success",
            value: {
              resolution: {
                resolution: {
                  case: "known",
                  value: {
                    method,
                    connectionId: "connection",
                    provider: { kind: 1, identifier: "openai" },
                    methodFacts:
                      method === AuthMethod.API_KEY
                        ? { case: "apiKey", value: { environmentVariable: "KEY" } }
                        : method === AuthMethod.NONE
                          ? { case: "noAuth", value: {} }
                          : { case: "oauth", value: {} },
                  },
                },
                ...(partialFailure === undefined ? {} : { partialFailure: { cause: partialFailure } }),
              },
              subscription:
                partialFailure !== undefined || method !== AuthMethod.OAUTH
                  ? { applicable: false }
                  : (subscription ?? {
                      applicable: true,
                      quotaSource: SubscriptionQuotaSource.CHATGPT,
                      windows: [
                        { bucket: SubscriptionWindowBucket.DEFAULT, durationSeconds: 18000, usedPercent: 25 },
                      ],
                    }),
            },
          },
        }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

test("optional plan and reset remain absent at the descriptor boundary", async () => {
  expect(await client(AuthMethod.OAUTH).getSubscriptionUsage("connection")).toMatchObject({
    plan: null,
    windows: [{ resetAt: null, limitName: null, meteredFeature: null, contributions: null }],
  });
});

test("supplied plan, reset and zero Grok contributions remain structured", async () => {
  const subscription = create(SubscriptionFactsSchema, {
    applicable: true,
    plan: "SuperGrok",
    quotaSource: SubscriptionQuotaSource.GROK,
    windows: [
      {
        bucket: SubscriptionWindowBucket.ADDITIONAL,
        durationSeconds: 7200,
        usedPercent: 30,
        resetAt: { seconds: 1900000000n, nanos: 0 },
        limitName: "premium",
        meteredFeature: "requests",
        grokContributions: { buildUsedPercent: 0, chatUsedPercent: 30 },
      },
    ],
  });
  expect(await client(AuthMethod.OAUTH, undefined, subscription).getSubscriptionUsage("connection")).toEqual({
    plan: "SuperGrok",
    source: "grok",
    windows: [
      {
        bucket: "additional",
        durationSeconds: 7200,
        usedPercent: 30,
        resetAt: 1900000000000,
        limitName: "premium",
        meteredFeature: "requests",
        contributions: { build: 0, chat: 30 },
      },
    ],
  });
});

test("unsupported credential failures are omitted while signed-out subscriptions remain inspectable", async () => {
  expect(await client(AuthMethod.API_KEY, "Missing key").getSubscriptionUsage("connection")).toBeNull();
  expect(await client(AuthMethod.NONE).getSubscriptionUsage("connection")).toBeNull();
  expect(
    await client(AuthMethod.API_KEY, "Missing key").getSubscriptionBinding(
      "9a033091-f67a-4e1d-9a6a-05caec0c03b0",
    ),
  ).toEqual({ connectionID: "connection", supported: false });
  expect(client(AuthMethod.OAUTH, "Signed out").getSubscriptionUsage("connection")).rejects.toMatchObject({
    kind: "auth",
  });
  expect(
    client(AuthMethod.OAUTH, "Signed out").getSubscriptionBinding("9a033091-f67a-4e1d-9a6a-05caec0c03b0"),
  ).rejects.toMatchObject({
    kind: "auth",
    binding: { connectionID: "connection", supported: true },
  });
});

test.each([
  {
    method: AuthService.method.getStatus,
    read: async (api: ApiClient) => api.getSubscriptionBinding("9a033091-f67a-4e1d-9a6a-05caec0c03b0"),
  },
  {
    method: AuthService.method.getStatus,
    read: async (api: ApiClient) => api.getSubscriptionUsage("connection"),
  },
  {
    method: AuthService.method.getConnections,
    read: async (api: ApiClient) => api.getSubscriptionConnections(),
  },
])("preserves supplied internal-failure diagnostics for Auth reads %#", async ({ method, read }) => {
  const cause = crypto.randomUUID();
  const transport = new FakeRpcTransport([
    {
      descriptor: method,
      result: create(method.output, {
        outcome: {
          case: "error",
          value: {
            code: "internal_failure",
            detail: { case: "internalFailure", value: { cause } },
          },
        },
      }),
    },
  ]);
  expect(read(new ApiClient(transport, unexpectedProjectOverflow))).rejects.toMatchObject({
    code: "internal_failure",
    message: cause,
  });
});
