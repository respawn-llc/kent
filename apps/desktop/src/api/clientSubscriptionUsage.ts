import { create } from "@app/server-api-contract";
import {
  AuthService,
  AuthMethod,
  ConnectionProtocol,
  SubscriptionQuotaSource,
  SubscriptionWindowBucket,
  SubscriptionFailureClassification,
  type Status,
} from "@app/server-api-contract/gen/kent/api/auth/auth_pb";
import { requireUnarySuccess } from "./protobufRpc";
import type { RpcTransport } from "./transport";
import { timestampMillis } from "./clientTime";
import { ContractError } from "./errors";
import { SubscriptionReadError, type SubscriptionUsage, type SubscriptionBinding } from "./subscriptionUsage";

export async function getSubscriptionConnections(transport: RpcTransport) {
  const method = AuthService.method.getConnections;
  const catalog = requireUnarySuccess(method, await transport.callDescriptor(method, create(method.input)));
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
  return requireUnarySuccess(method, await transport.callDescriptor(method, create(method.input, target)));
}

function binding(value: Status): SubscriptionBinding {
  const resolution = value.resolution;
  if (resolution?.resolution.case !== "known") {
    throw new SubscriptionReadError(
      resolution?.resolution.case === "unavailable"
        ? resolution.resolution.value.cause
        : "Session connection is unavailable.",
      "binding",
    );
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
  if (!binding(value).supported) return null;
  const subscription = value.subscription;
  if (subscription === undefined) throw new ContractError("Subscription facts are missing.");
  const source =
    subscription.quotaSource === SubscriptionQuotaSource.CHATGPT
      ? "chatgpt"
      : subscription.quotaSource === SubscriptionQuotaSource.GROK
        ? "grok"
        : null;
  if (subscription.failure !== undefined)
    throw new SubscriptionReadError(subscription.failure.cause, "quota", {
      source,
      classification:
        subscription.failureClassification ===
        SubscriptionFailureClassification.UNAVAILABLE_FOR_SIGN_IN_METHOD
          ? "unavailable-for-sign-in-method"
          : null,
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
