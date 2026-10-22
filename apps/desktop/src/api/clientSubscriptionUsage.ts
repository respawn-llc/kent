import { create, type DescMethod } from "@app/server-api-contract";
import {
  AuthService,
  AuthMethod,
  ConnectionProtocol,
  SubscriptionQuotaSource,
  SubscriptionWindowBucket,
  SubscriptionFailureClassification,
  type Status,
  type GetStatusResult,
  type GetConnectionsResult,
} from "@app/server-api-contract/gen/kent/api/auth/auth_pb";
import { protobufRpcError, requireUnarySuccess } from "./protobufRpc";
import type { RpcTransport } from "./transport";
import { timestampMillis } from "./clientTime";
import { RpcError } from "./errors";
import { SubscriptionReadError, type SubscriptionUsage, type SubscriptionBinding } from "./subscriptionUsage";

export async function getSubscriptionConnections(transport: RpcTransport) {
  const method = AuthService.method.getConnections;
  const result = await transport.callDescriptor(method, create(method.input));
  throwAuthReadFailure(method, result.outcome);
  const catalog = requireUnarySuccess(method, result);
  return catalog.connections.map((connection) => ({
    id: connection.id,
    supported:
      connection.protocol === ConnectionProtocol.CHATGPT ||
      connection.protocol === ConnectionProtocol.GROK_CLI_PROXY ||
      connection.protocol === ConnectionProtocol.GROK_OAUTH_API,
  }));
}

async function status(
  transport: RpcTransport,
  target: { sessionId: string; skipSubscriptionUsage: true } | { provider: { connectionId: string } },
) {
  const method = AuthService.method.getStatus;
  const result = await transport.callDescriptor(method, create(method.input, target));
  throwAuthReadFailure(method, result.outcome);
  return requireUnarySuccess(method, result);
}

function throwAuthReadFailure(
  method: DescMethod,
  outcome: (GetStatusResult | GetConnectionsResult)["outcome"],
): void {
  if (outcome.case !== "error" || outcome.value.detail.case !== "internalFailure") return;
  const error = protobufRpcError(method, outcome.value);
  throw new RpcError({
    code: error.code,
    method: error.method,
    data: error.data,
    message: outcome.value.detail.value.cause ?? error.message,
  });
}

function binding(value: Status): SubscriptionBinding | null {
  const resolution = value.resolution;
  if (resolution?.resolution.case === "unavailable") return null;
  if (resolution?.resolution.case !== "known") {
    throw new SubscriptionReadError("Session connection is unavailable.", "binding");
  }
  const facts = resolution.resolution.value;
  const result = { connectionID: facts.connectionId, supported: facts.method === AuthMethod.OAUTH };
  // Unsupported credentials can also carry partial failures; omit them first.
  if (!result.supported) return result;
  if (resolution.partialFailure !== undefined)
    throw new SubscriptionReadError(resolution.partialFailure.cause, "auth", { binding: result });
  return result;
}

export async function getSubscriptionBinding(transport: RpcTransport, sessionID: string) {
  return binding(await status(transport, { sessionId: sessionID, skipSubscriptionUsage: true }));
}

export async function getSubscriptionUsage(
  transport: RpcTransport,
  connectionID: string,
): Promise<SubscriptionUsage | null> {
  const value = await status(transport, { provider: { connectionId: connectionID } });
  const subscription = value.subscription;
  if (
    subscription.failureClassification === SubscriptionFailureClassification.UNAVAILABLE_FOR_SIGN_IN_METHOD ||
    binding(value)?.supported !== true
  )
    return null;
  const source =
    subscription.quotaSource === SubscriptionQuotaSource.CHATGPT
      ? "chatgpt"
      : subscription.quotaSource === SubscriptionQuotaSource.GROK
        ? "grok"
        : null;
  if (subscription.failure !== undefined)
    throw new SubscriptionReadError(subscription.failure.cause, "quota", {
      source,
    });
  if (!subscription.applicable) return null;
  return {
    plan: subscription.plan ?? null,
    source,
    windows: subscription.windows.map((window) => ({
      bucket: window.bucket === SubscriptionWindowBucket.ADDITIONAL ? "additional" : "default",
      durationSeconds: window.durationSeconds,
      usedPercent: window.usedPercent,
      resetAt: window.resetAt === undefined ? null : timestampMillis(window.resetAt),
      limitName: window.limitName ?? null,
      meteredFeature: window.meteredFeature ?? null,
      contributions:
        window.grokContributions === undefined
          ? null
          : {
              build: window.grokContributions.buildUsedPercent ?? null,
              chat: window.grokContributions.chatUsedPercent ?? null,
            },
    })),
  };
}
