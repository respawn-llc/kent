import { useCallback, useState, type ReactNode } from "react";

import { errorMessage } from "../api/errors";
import { useStatusController } from "./useStatusController";

export type NativeDialogFallbackController<TPayload> = Readonly<{
  fallback: ReactNode;
  open(payload: TPayload): Promise<void>;
}>;

export function useNativeDialogFallback<TPayload>({
  errorNoticeID,
  errorTitle,
  openNative,
  renderFallback,
}: Readonly<{
  errorNoticeID: string;
  errorTitle: string;
  openNative: (payload: TPayload) => Promise<void>;
  renderFallback: (payload: TPayload, close: () => void) => ReactNode;
}>): NativeDialogFallbackController<TPayload> {
  const { push } = useStatusController();
  const [fallbackPayload, setFallbackPayload] = useState<TPayload | null>(null);
  const closeFallback = useCallback(() => {
    setFallbackPayload(null);
  }, []);
  const open = useCallback(
    async (payload: TPayload): Promise<void> => {
      try {
        await openNative(payload);
        setFallbackPayload(null);
      } catch (error) {
        push({
          id: errorNoticeID,
          tone: "danger",
          title: errorTitle,
          body: errorMessage(error),
        });
        setFallbackPayload(payload);
      }
    },
    [errorNoticeID, errorTitle, openNative, push],
  );

  return {
    fallback: fallbackPayload === null ? null : renderFallback(fallbackPayload, closeFallback),
    open,
  };
}
