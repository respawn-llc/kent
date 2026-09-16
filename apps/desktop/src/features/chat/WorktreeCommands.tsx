import { useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { WorktreeDeletePreview } from "@/api";
import {
  createRefreshOpenWorktreeList,
  useAppServices,
  useOwnedSidebarRoots,
  useSidebarShell,
  useStatusController,
} from "@/app-facade";
import { Spinner } from "@/ui";
import type { ComposerCommand } from "./composerCommands";
import { createWorktreeCommand } from "./worktreeCommand";
import { createWorktreeCommandActions } from "./WorktreeCommandActions";
import { createWorktreeCommandDelete, useWorktreeCommandDelete } from "./WorktreeCommandDelete";
import { WorktreeCommandDeleteDialog } from "./WorktreeCommandDeleteDialog";
import { useWorktreeActions } from "./WorktreeActions";

type Props = Readonly<{
  sessionID: string | null;
  focusComposer(): void;
  children(commands: readonly ComposerCommand[]): ReactNode;
}>;

export function WorktreeCommands(props: Props) {
  return props.sessionID === null ? (
    <NewChatWorktreeCommands>{props.children}</NewChatWorktreeCommands>
  ) : (
    <SessionWorktreeCommands key={props.sessionID} {...props} sessionID={props.sessionID} />
  );
}

function NewChatWorktreeCommands({ children }: Pick<Props, "children">) {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const commands = useMemo(() => [createWorktreeCommand({ execute: null, push, t })], [push, t]);
  return <>{children(commands)}</>;
}

function SessionWorktreeCommands({ sessionID, focusComposer, children }: Props & { sessionID: string }) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const roots = useOwnedSidebarRoots();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const [preview, setPreview] = useState<WorktreeDeletePreview | null>(null);
  const close = useCallback(() => {
    setPreview(null);
    focusComposer();
  }, [focusComposer]);
  const model = useMemo(() => {
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
        setPreview(value);
      },
    });
    const actions = createWorktreeCommandActions({
      ...dependencies,
      roots,
      focusComposer,
      deleteTarget: deletion.prepare,
    });
    const commands = [
      createWorktreeCommand({ push, t, execute: async (_sessionID, intent) => actions.execute(intent) }),
    ];
    return { deletion, actions, commands };
  }, [api, client, currentSurface, focusComposer, push, roots, sessionID, t]);
  useWorktreeActions(model.actions.transitions);
  const { confirm } = useWorktreeCommandDelete(model.deletion);
  const preparing = useAtomValue(model.deletion.requestPending);
  const deleting = useAtomValue(model.deletion.deletion.requestPending);
  const switching = useAtomValue(model.actions.transitions.requestPending);
  return (
    <>
      {children(model.commands)}
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
