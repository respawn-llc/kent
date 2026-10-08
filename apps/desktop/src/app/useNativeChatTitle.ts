import { useEffect } from "react";
import { errorMessage } from "@/api";
import { useAppServices } from "@/app-facade";

export function useNativeChatTitle(title: string | null) {
  const { nativeBridge, logger } = useAppServices();
  useEffect(() => {
    if (title === null) return;
    void nativeBridge.window
      .setCurrentTitle(title)
      .catch(async (error: unknown) =>
        logger.append("warn", "Native Chat title update failed.", { error: errorMessage(error) }),
      );
  }, [nativeBridge, logger, title]);
}
