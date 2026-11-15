import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { WorktreeDeletePreview } from "@/api";
import {
  createRefreshOpenWorktreeList,
  useAppServices,
  useOwnedSidebarRoots,
  useSidebarShell,
  useStatusController,
  worktreeTransitionOutcomeHandler,
  type ChatWorktreeBinding,
} from "@/app-facade";
import { Spinner, useStableCallback } from "@/ui";
import { createWorktreeCommand } from "./worktreeCommand";
import { createWorktreeCommandActions } from "./WorktreeCommandActions";
import { createWorktreeCommandDelete, useWorktreeCommandDelete } from "./WorktreeCommandDelete";
import { WorktreeCommandDeleteDialog } from "./WorktreeCommandDeleteDialog";
import { useWorktreeActions } from "./WorktreeActions";

type WorktreeCommandModel = Readonly<{
  deletion: ReturnType<typeof createWorktreeCommandDelete>;
  actions: NonNullable<Atom.Type<ReturnType<typeof createWorktreeCommandActions>>>;
  preview: Atom.Writable<WorktreeDeletePreview | null>;
}>;

export function useWorktreeCommands({
  projectID,
  focusComposer,
  openSession,
  chat,
}: Readonly<{
  projectID: string;
  focusComposer(): void;
  openSession(sessionID: string): void | Promise<void>;
  chat: ChatWorktreeBinding;
}>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const roots = useOwnedSidebarRoots();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const openOriginSession = useStableCallback(openSession);
  const managementScope = useAtomValue(chat.scope);
  const sessionID = managementScope?.kind === "session" ? managementScope.sessionID : null;
  const temporarySelector = useAtomValue(chat.selector);
  const scope = useMemo(
    () =>
      Atom.make((get) => {
        if (managementScope === null) return null;
        const preview = Atom.make<WorktreeDeletePreview | null>(null);
        const dependencies = {
          client,
          api,
          projectID,
          scope: managementScope,
          push,
          t,
          refreshOpenWorktreeList: createRefreshOpenWorktreeList(client, api, currentSurface),
        };
        const deletion = createWorktreeCommandDelete({
          ...dependencies,
          present: (value) => {
            focusComposer();
            get.set(preview, value);
          },
        });
        const actions = get(
          createWorktreeCommandActions({
            client,
            api,
            projectID,
            push,
            t,
            chat,
            refreshOpenWorktreeList: dependencies.refreshOpenWorktreeList,
            roots,
            focusComposer,
            openSession: openOriginSession,
            deleteTarget: async (selector) => deletion.prepare(selector ?? temporarySelector),
          }),
        );
        if (actions === null) return null;
        return {
          deletion,
          actions,
          preview,
          observation:
            sessionID === null
              ? undefined
              : {
                  onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(
                    dependencies.refreshOpenWorktreeList,
                    sessionID,
                    push,
                    t,
                  ),
                },
        };
      }),
    [
      api,
      client,
      currentSurface,
      focusComposer,
      openOriginSession,
      projectID,
      push,
      roots,
      sessionID,
      t,
      managementScope,
      chat,
      temporarySelector,
    ],
  );
  const model = useAtomValue(scope);
  const commands = useMemo(
    () =>
      model === null
        ? []
        : [
            createWorktreeCommand({
              push,
              t,
              execute: async (_sessionID, intent) => model.actions.execute(intent),
            }),
          ],
    [model, push, t],
  );
  return {
    commands,
    observation: model?.observation,
    presentation:
      model === null ? null : (
        <WorktreeCommandPresentation key={sessionID} model={model} focusComposer={focusComposer} />
      ),
  };
}

function WorktreeCommandPresentation({
  model,
  focusComposer,
}: Readonly<{ model: WorktreeCommandModel; focusComposer(): void }>) {
  useWorktreeActions(model.actions.transitions);
  const { confirm } = useWorktreeCommandDelete(model.deletion);
  const preparing = useAtomValue(model.deletion.requestPending);
  const deleting = useAtomValue(model.deletion.deletion.requestPending);
  const switching = useAtomValue(model.actions.transitions.requestPending);
  const preview = useAtomValue(model.preview);
  const setPreview = useAtomSet(model.preview);
  const close = () => {
    setPreview(null);
    focusComposer();
  };
  return (
    <>
      {preparing || switching || deleting ? (
        <div className="flex items-center gap-[var(--space-2)] text-sm">
          <Spinner size="sm" />
        </div>
      ) : null}
      {preview === null ? null : (
        <WorktreeCommandDeleteDialog
          preview={preview}
          onDismiss={close}
          onChoice={(choice) => {
            close();
            queueMicrotask(() => {
              confirm({ preview, choice });
            });
          }}
        />
      )}
    </>
  );
}
