import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useAppServices } from "@/app-facade";
import type { ChatSettingsTarget, ChatContext, InitialChatSettings } from "@/api";
import { createChatSettingsViewModel } from "./ChatSettingsViewModel";
import { useChatSettings as useSettings, type ChatSettingsNavigation } from "./useChatSettings";

export type ChatSettingsOptions =
  | Readonly<{
      target: Extract<ChatSettingsTarget, { kind: "new_chat" }>;
      onInitialSettingsChange(settings: InitialChatSettings): void;
    }>
  | (ChatSettingsNavigation &
      Readonly<{
        target: Extract<ChatSettingsTarget, { kind: "session" }>;
        authoritativeRefreshGeneration: unknown;
        onContextChange(context: ChatContext): void;
      }>);

export function useChatSettings(options: ChatSettingsOptions) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const { target } = options;
  const projectID = target.projectID;
  const sessionID = target.kind === "session" ? target.sessionID : null;
  const workspaceID =
    target.kind === "new_chat" && "workspaceID" in target.workspace ? target.workspace.workspaceID : null;
  const workspaceRoot =
    target.kind === "new_chat" && "workspaceRoot" in target.workspace ? target.workspace.workspaceRoot : null;
  const selected = useMemo<ChatSettingsTarget>(() => {
    if (sessionID !== null) return { kind: "session", projectID, sessionID };
    if (workspaceID !== null) return { kind: "new_chat", projectID, workspace: { workspaceID } };
    if (workspaceRoot !== null) return { kind: "new_chat", projectID, workspace: { workspaceRoot } };
    throw new Error("Missing test workspace");
  }, [projectID, sessionID, workspaceID, workspaceRoot]);
  const [targetAtom] = useState(() => Atom.make(selected));
  const active = useAtomValue(targetAtom);
  const setTarget = useAtomSet(targetAtom);
  useLayoutEffect(() => {
    setTarget(selected);
  }, [selected, setTarget]);
  const [model] = useState(() => createChatSettingsViewModel({ services, client, t, target: targetAtom }));
  const refresh = useAtomSet(model.refresh);
  const state = useAtomValue(model.state);
  const generation =
    "authoritativeRefreshGeneration" in options ? options.authoritativeRefreshGeneration : null;
  const previous = useRef({ selected, generation });
  useEffect(() => {
    const old = previous.current;
    previous.current = { selected, generation };
    if (old.selected === selected && old.generation !== generation) refresh(undefined);
  }, [selected, generation, refresh]);
  const report = "onInitialSettingsChange" in options ? options.onInitialSettingsChange : undefined;
  useEffect(() => {
    if (state.kind === "ready-new-chat") report?.(state.initialSettings);
  }, [state, report]);
  const feature = useSettings({
    model,
    ...testNavigation(options),
    ...("onContextChange" in options ? { onContextChange: options.onContextChange } : {}),
  });
  return active === selected
    ? feature
    : {
        kind: selected.kind === "session" ? ("loading-session" as const) : ("loading-new-chat" as const),
        retry: () => {
          refresh(undefined);
        },
      };
}

function testNavigation(options: ChatSettingsOptions): ChatSettingsNavigation {
  return "openTask" in options ? options : { openTask: () => undefined, openParentSession: () => undefined };
}
