import { useSyncExternalStore } from "react";

import { useAppServices } from "./useAppServices";

export function useConnectionSnapshot() {
  const { api } = useAppServices();
  return useSyncExternalStore(
    (listener) => api.transport.connection.subscribe(listener),
    () => api.transport.connection.snapshot(),
    () => api.transport.connection.snapshot(),
  );
}
