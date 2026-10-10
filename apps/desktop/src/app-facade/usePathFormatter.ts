import { useCallback } from "react";

import { formatHomeRelativePath } from "./formatters";
import { useAppServices } from "./useAppServices";

export function usePathFormatter(): (path: string) => string {
  const { homePath, nativeBridge } = useAppServices();
  const platform = nativeBridge.capabilities.platform;
  return useCallback(
    (path: string) => formatHomeRelativePath(path, homePath, platform),
    [homePath, platform],
  );
}
