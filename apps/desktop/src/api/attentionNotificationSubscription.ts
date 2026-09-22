import type { AttentionObservation } from "./attentionNotifications";
import { ContractError } from "./errors";
import { parseRpcResponse } from "./clientParse";
import { attentionNotificationEventParamsSchema } from "./schemas/attentionNotification";
import type { DescriptorRpcTransport, RpcEventHandler } from "./transport";
import { subscriptionStream } from "./subscriptionStream";

export function attentionNotifications(
  transport: DescriptorRpcTransport,
  reportOverflow: () => Promise<void>,
) {
  return subscriptionStream<AttentionObservation>(
    (emit) =>
      transport.subscribe("attention.notification.subscribe", {}, attentionNotificationRpcHandler(emit)),
    reportOverflow,
  );
}

function attentionNotificationRpcHandler(emit: (value: AttentionObservation) => void): RpcEventHandler {
  return {
    onOpen: () => {
      emit({ kind: "open" });
    },
    onComplete: (code, message) => {
      emit({ kind: "complete", code, message });
    },
    onError: (error) => {
      emit({ kind: "error", error });
    },
    onEvent(method, params) {
      if (method !== "attention.notification") {
        return;
      }
      let parsed;
      try {
        parsed = parseRpcResponse(method, attentionNotificationEventParamsSchema, params);
      } catch (error) {
        if (!(error instanceof ContractError)) throw error;
        emit({ kind: "error", error });
        return;
      }
      emit({ kind: "event", event: parsed.event });
    },
  };
}
