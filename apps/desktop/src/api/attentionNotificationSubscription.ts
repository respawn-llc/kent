import type { AttentionNotificationEventHandler } from "./attentionNotifications";
import { ContractError } from "./errors";
import { parseRpcResponse } from "./clientParse";
import { attentionNotificationEventParamsSchema } from "./schemas/attentionNotification";
import type { RpcEventHandler } from "./transport";

export function attentionNotificationRpcHandler(handler: AttentionNotificationEventHandler): RpcEventHandler {
  return {
    ...(handler.onOpen !== undefined ? { onOpen: handler.onOpen } : {}),
    onComplete: handler.onComplete,
    onError: handler.onError,
    onEvent(method, params) {
      if (method !== "attention.notification") {
        return;
      }
      let parsed;
      try {
        parsed = parseRpcResponse(method, attentionNotificationEventParamsSchema, params);
      } catch (error) {
        if (!(error instanceof ContractError)) throw error;
        handler.onError(error);
        return;
      }
      handler.onEvent(parsed.event);
    },
  };
}
