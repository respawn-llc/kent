import { ContractError, RpcError, TransportError } from "./errors";

export class TerminalSubscriptionError extends Error {}

export class InvalidTranscriptEventError extends Error {
  constructor(readonly contractError: Error) {
    super(contractError.message, { cause: contractError });
  }
}

export function handleDescriptorSubscriptionFailure(
  cause: unknown,
  operation: string,
  onError: (error: Error) => void,
  onInvalidEvent: ((error: Error) => void) | undefined,
): Error | undefined {
  if (onInvalidEvent === undefined) {
    return cause instanceof Error
      ? cause
      : new TransportError(`${operation} subscription failed.`, { cause });
  }
  if (cause instanceof InvalidTranscriptEventError) {
    try {
      onInvalidEvent(cause.contractError);
      return undefined;
    } catch (callbackError) {
      return new TerminalSubscriptionError(
        callbackError instanceof Error ? callbackError.message : "Subscription error handler failed.",
        { cause: callbackError },
      );
    }
  }
  let failure: Error =
    cause instanceof RpcError || cause instanceof ContractError
      ? cause
      : new ContractError(
          cause instanceof Error ? cause.message : "Transcript subscription is invalid.",
          [],
          0,
          { cause },
        );
  try {
    onError(failure);
  } catch (callbackError) {
    failure =
      callbackError instanceof Error
        ? callbackError
        : new ContractError("Subscription error handler failed.", [], 0, { cause: callbackError });
  }
  return new TerminalSubscriptionError(failure.message, { cause: failure });
}
