import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAppServices } from "@/app-facade";
import { createSubscriptionUsageModel } from "./model";

export function useSubscriptionUsageModel(sessionID: string | null) {
  const { api } = useAppServices();
  const client = useQueryClient();
  return useMemo(() => createSubscriptionUsageModel({ api, client, sessionID }), [api, client, sessionID]);
}
