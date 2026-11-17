import { useContext, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import {
  SidebarShellContext,
  useAppServices,
  useStatusController,
  type SidebarDestination,
} from "@/app-facade";
import { Spinner } from "@/ui";
import { createNewSessionShortcutViewModel, type NewSessionSource } from "./NewSessionShortcutViewModel";

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
  const request = useAtomValue(model.request);
  const activate = useAtomSet(model.activate);
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
      if (!event.repeat && effective !== null)
        activate({ source: effective, newWindow: event.shiftKey, openNewChat });
    };
    window.addEventListener("keydown", listener);
    return () => {
      window.removeEventListener("keydown", listener);
    };
  }, [activate, effective, openNewChat, services.nativeBridge.capabilities.platform]);
  return request.isPending ? (
    <div className="pointer-events-none fixed inset-0 z-50 grid place-items-center">
      <Spinner testID="new-session-opening" />
    </div>
  ) : null;
}

function sidebarSource(destination: SidebarDestination): NewSessionSource | null {
  if (destination.kind === "taskDetail") return { kind: "task", taskID: destination.taskID };
  if ("projectID" in destination && destination.projectID !== undefined)
    return { kind: "project", projectID: destination.projectID };
  return null;
}
