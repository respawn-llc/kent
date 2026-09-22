import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
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
} from "@/app-facade";
import { Spinner } from "@/ui";
import { createWorktreeCommand } from "./worktreeCommand";
import { createWorktreeCommandActions } from "./WorktreeCommandActions";
import { createWorktreeCommandDelete, useWorktreeCommandDelete } from "./WorktreeCommandDelete";
import { WorktreeCommandDeleteDialog } from "./WorktreeCommandDeleteDialog";
import { useWorktreeActions } from "./WorktreeActions";

type WorktreeCommandModel = Readonly<{
  deletion: ReturnType<typeof createWorktreeCommandDelete>;
  actions: ReturnType<typeof createWorktreeCommandActions>;
  preview: Atom.Writable<WorktreeDeletePreview | null>;
}>;

export function useWorktreeCommands(sessionID: string | null, focusComposer: () => void) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const roots = useOwnedSidebarRoots();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const scope = useMemo(
    () =>
      Atom.make((get) => {
        if (sessionID === null) return null;
        const preview = Atom.make<WorktreeDeletePreview | null>(null);
        const dependencies = {
          client,
          api,
          sessionID,
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
        const actions = createWorktreeCommandActions({
          ...dependencies,
          roots,
          focusComposer,
          deleteTarget: deletion.prepare,
        });
        return {
          deletion,
          actions,
          preview,
          observation: {
            onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(
              dependencies.refreshOpenWorktreeList,
              sessionID,
              push,
              t,
            ),
          },
        };
      }),
    [api, client, currentSurface, focusComposer, push, roots, sessionID, t],
  );
  const model = useAtomValue(scope);
  const commands = useMemo(
    () => [
      createWorktreeCommand({
        push,
        t,
        execute: model === null ? null : async (_sessionID, intent) => model.actions.execute(intent),
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
  const { t } = useTranslation();
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
          {t("chat.worktree.requestPending")}
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
