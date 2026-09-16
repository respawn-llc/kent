import { useLayoutEffect, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { ChatGoalSetTarget, ChatGoalAvailability, ChatSettingsTarget } from "@/api";
import { useAppServices } from "@/app-facade";
import { NewChatGoalBinding, type NewChatGoalHostDelivery } from "./goalBinding";
import { useGoalSidebarLauncher } from "./useGoalSidebarLauncher";

export function useChatGoal(
  options: Readonly<{
    target: ChatSettingsTarget;
    ready: boolean;
    availability: ChatGoalAvailability | null;
    captureTarget(): Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
    delivered(delivery: NewChatGoalHostDelivery): void;
    failed(): void;
  }>,
) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const [binding] = useState(
    () =>
      new NewChatGoalBinding({
        client,
        api: api.chat,
        captureTarget: () => options.captureTarget(),
        onHostDelivery: (delivery) => {
          options.delivered(delivery);
        },
        onFailure: () => {
          options.failed();
        },
      }),
  );
  useLayoutEffect(() => {
    binding.updateHost({
      captureTarget: () => options.captureTarget(),
      onHostDelivery: (delivery) => {
        options.delivered(delivery);
      },
      onFailure: () => {
        options.failed();
      },
    });
  }, [binding, options]);
  useLayoutEffect(() => {
    if (options.target.kind === "session") binding.followSession(options.target);
    else {
      binding.setReady(options.ready);
      binding.setAvailability(options.availability);
    }
  }, [binding, options.target, options.ready, options.availability]);
  const pending = useSyncExternalStore(
    (listener) => binding.subscribe(listener),
    () => binding.pending,
    () => binding.pending,
  );
  const open = useGoalSidebarLauncher({ kind: "new_chat", api: api.chat, binding });
  return { open, pending } as const;
}
