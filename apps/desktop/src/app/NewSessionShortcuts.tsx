import { useContext, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import {
  SidebarShellContext,
  useAppServices,
  useStatusController,
  type SidebarDestination,
} from "@/app-facade";
import { createNewSessionShortcutViewModel, type NewSessionSource } from "./NewSessionShortcutViewModel";
import { useStableCallback } from "@/ui";

export function NewSessionShortcuts({
  source,
  openNewChat,
}: Readonly<{ source: NewSessionSource | null; openNewChat(projectID: string): Promise<void> }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const { push } = useStatusController();
  const sidebar = useContext(SidebarShellContext);
  const destination = sidebar?.activeDestination;
  const effective = destination == null ? source : sidebarSource(destination);
  const [model] = useState(() => createNewSessionShortcutViewModel({ services, client, t, push }));
  useAtomMount(model.request);
  const activate = useAtomSet(model.activate);
  const currentDestination = useStableCallback(() => ({ source: effective, openNewChat }));
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      const mac = services.nativeBridge.capabilities.platform === "macos";
      if (
        event.defaultPrevented ||
        event.isComposing ||
        event.altKey ||
        event.key.toLowerCase() !== "n" ||
        !(mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey)
      )
        return;
      event.preventDefault();
      if (!event.repeat) activate({ currentDestination, newWindow: event.shiftKey });
    };
    window.addEventListener("keydown", listener);
    return () => {
      window.removeEventListener("keydown", listener);
    };
  }, [activate, currentDestination, services.nativeBridge.capabilities.platform]);
  return null;
}

function sidebarSource(destination: SidebarDestination): NewSessionSource | null {
  if (destination.kind === "taskDetail") return { kind: "task", taskID: destination.taskID };
  if ("projectID" in destination && destination.projectID !== undefined)
    return { kind: "project", projectID: destination.projectID };
  return null;
}
