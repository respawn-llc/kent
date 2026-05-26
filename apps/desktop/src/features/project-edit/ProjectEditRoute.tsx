import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";

import type { ProjectEdit, WorkspaceSummary } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { ErrorState, LoadingState, TextInput, VirtualizedInfiniteList } from "../../ui";
import {
  ProjectNameField,
  WorkspaceRow,
  WorkspaceUnlinkFallbackDialog,
  type WorkspaceUnlinkTarget,
  workspaceUnlinkDialogWidth,
} from "./ProjectEditParts";
import { findWorkspaceByPath, projectNameErrors } from "./ProjectEditUtils";
import {
  useProjectDefaultWorkspaceSave,
  useProjectWorkspaceChangedEvents,
  useProjectEdit,
  useProjectNameSave,
  useProjectWorkspaceAttach,
  useProjectWorkspaceUnlinkRequests,
  useProjectWorkspaceUnlink,
} from "./useProjectEditData";

const projectEditContentMaxWidthClassName = "max-w-[1200px]";

export function ProjectEditRoute({ projectId }: Readonly<{ projectId: string }>) {
  const { t } = useTranslation();
  const query = useProjectEdit(projectId);
  const pages = query.data?.pages;
  const project = pages?.[0];
  const workspaces = useMemo(() => pages?.flatMap((page) => page.workspaces) ?? [], [pages]);
  useWindowChromeTitle(project?.displayName ?? null);

  if (query.isPending) {
    return <LoadingState body={t("states.loading")} reveal={false} title={t("projectEdit.loadingTitle")} />;
  }

  if (query.isError || project === undefined) {
    return (
      <ErrorState
        body={query.isError ? errorMessage(query.error) : t("projectEdit.missingProject")}
        onRetry={() => void query.refetch()}
        reveal={false}
        retryLabel={t("app.retry")}
        title={t("states.error")}
      />
    );
  }

  return (
    <ProjectEditContent
      hasNextPage={query.hasNextPage}
      isFetchingNextPage={query.isFetchingNextPage}
      key={project.projectID}
      onLoadMore={() => void query.fetchNextPage()}
      project={project}
      workspaces={workspaces}
    />
  );
}

function ProjectEditContent({
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  project,
  workspaces,
}: Readonly<{
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
  project: ProjectEdit;
  workspaces: readonly WorkspaceSummary[];
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const connection = useConnectionSnapshot();
  const nameSave = useProjectNameSave(project.projectID);
  const defaultSave = useProjectDefaultWorkspaceSave(project.projectID);
  const attach = useProjectWorkspaceAttach(project.projectID);
  const unlink = useProjectWorkspaceUnlink(project.projectID);
  const [nameDraft, setNameDraft] = useState(project.displayName);
  const disabled = connection.phase !== "connected";
  const mutating =
    disabled || nameSave.isPending || defaultSave.isPending || attach.isPending || unlink.isPending;
  const nameErrors = projectNameErrors(nameDraft, t);
  const nameChanged = nameDraft !== project.displayName;
  const pushToast = useCallback(
    (id: string, tone: "info" | "success" | "danger", body: string, title = t("projectEdit.title")) => {
      push({ id, tone, title, body });
    },
    [push, t],
  );
  const confirmUnlink = useCallback(
    async (target: WorkspaceUnlinkTarget, close?: () => void): Promise<void> => {
      try {
        const response = await unlink.mutateAsync(target.workspaceID);
        if (response.unlinked) {
          close?.();
          pushToast("project-edit-workspace-unlinked", "success", t("projectEdit.workspaceUnlinked"));
          return;
        }
        pushToast(
          "project-edit-workspace-unlink-blocked",
          "danger",
          response.blockers.map((blocker) => blocker.message).join("\n") ||
            t("projectEdit.workspaceUnlinkBlocked"),
          t("projectEdit.workspaceUnlinkBlocked"),
        );
      } catch (error) {
        pushToast("project-edit-workspace-unlink-error", "danger", errorMessage(error));
      }
    },
    [pushToast, t, unlink],
  );
  const unlinkDialog = useNativeDialogFallback<WorkspaceUnlinkTarget>({
    errorNoticeID: "workspace-unlink-window-error",
    errorTitle: t("projectEdit.unlinkWindowError"),
    nativeAvailable: nativeBridge.capabilities.dialogWindows,
    openNative: async (target) => {
      await nativeBridge.dialogs.openWindow(
        workspaceUnlinkWindowOptions(target, t("projectEdit.unlinkTitle")),
      );
    },
    renderFallback: (target, close) => (
      <WorkspaceUnlinkFallbackDialog
        disabled={mutating}
        onClose={close}
        onConfirm={(nextTarget) => void confirmUnlink(nextTarget, close)}
        target={target}
      />
    ),
  });
  const handleWorkspaceUnlinkRequest = useCallback(
    (target: WorkspaceUnlinkTarget) => {
      if (target.projectID === project.projectID) {
        void confirmUnlink(target);
      }
    },
    [confirmUnlink, project.projectID],
  );

  useProjectWorkspaceUnlinkRequests(nativeBridge, handleWorkspaceUnlinkRequest);
  useProjectWorkspaceChangedEvents(nativeBridge, project.projectID);

  async function chooseWorkspace(): Promise<void> {
    try {
      const selected = await nativeBridge.directories.selectDirectory({
        title: t("projectEdit.chooseWorkspace"),
      });
      if (selected === null) {
        return;
      }
      const loadedMatch = findWorkspaceByPath(workspaces, selected.path);
      if (loadedMatch !== undefined) {
        pushToast("project-edit-workspace-duplicate", "info", t("projectEdit.workspaceAlreadyLinked"));
        return;
      }
      await attach.mutateAsync(selected.path);
      pushToast("project-edit-workspace-attached", "success", t("projectEdit.workspaceAttached"));
    } catch (error) {
      pushToast("project-edit-workspace-attach-error", "danger", errorMessage(error));
    }
  }

  async function saveName(): Promise<void> {
    try {
      await nameSave.mutateAsync(nameDraft);
      pushToast("project-edit-name-saved", "success", t("projectEdit.projectSaved"));
    } catch (error) {
      pushToast("project-edit-name-save-error", "danger", errorMessage(error));
    }
  }

  async function saveDefaultWorkspace(workspace: WorkspaceSummary): Promise<void> {
    if (workspace.id === project.defaultWorkspaceID) {
      return;
    }
    try {
      await defaultSave.mutateAsync(workspace.id);
      pushToast("project-edit-default-saved", "success", t("projectEdit.defaultWorkspaceSaved"));
    } catch (error) {
      pushToast("project-edit-default-save-error", "danger", errorMessage(error));
    }
  }

  const header = (
    <ProjectEditListHeader
      disabled={mutating}
      nameChanged={nameChanged}
      nameDraft={nameDraft}
      nameErrors={nameErrors}
      onAttach={() => void chooseWorkspace()}
      onNameChange={setNameDraft}
      onNameSave={() => void saveName()}
      project={project}
    />
  );

  return (
    <section
      aria-labelledby="workspaces-title"
      className="island-glass h-full min-h-0 overflow-hidden rounded-[var(--radius-xl)]"
      data-testid="project-edit-route"
    >
      {unlinkDialog.fallback}
      <ProjectWorkspaceList
        defaultWorkspaceID={project.defaultWorkspaceID}
        disabled={mutating}
        hasNextPage={hasNextPage}
        header={header}
        isFetchingNextPage={isFetchingNextPage}
        onLoadMore={onLoadMore}
        onMakeDefault={(workspace) => void saveDefaultWorkspace(workspace)}
        onUnlink={(workspace) => {
          void unlinkDialog.open({
            projectID: project.projectID,
            rootPath: workspace.rootPath,
            workspaceID: workspace.id,
          });
        }}
        workspaces={workspaces}
      />
    </section>
  );
}

function ProjectEditListHeader({
  disabled,
  nameChanged,
  nameDraft,
  nameErrors,
  onAttach,
  onNameChange,
  onNameSave,
  project,
}: Readonly<{
  disabled: boolean;
  nameChanged: boolean;
  nameDraft: string;
  nameErrors: readonly string[];
  onAttach: () => void;
  onNameChange: (value: string) => void;
  onNameSave: () => void;
  project: ProjectEdit;
}>) {
  const { t } = useTranslation();
  return (
    <div className={`mx-auto grid w-full ${projectEditContentMaxWidthClassName} gap-[var(--space-3)]`}>
      <div className="grid min-w-0 gap-[var(--space-3)]">
        <ProjectNameField
          disabled={disabled}
          nameChanged={nameChanged}
          nameDraft={nameDraft}
          nameErrors={nameErrors}
          onNameChange={onNameChange}
          onNameSave={onNameSave}
        />
        <TextInput disabled label={t("home.projectKey")} value={project.projectKey} />
      </div>
      <div className="flex min-w-0 items-center justify-between gap-[var(--space-3)]">
        <h1 className="m-0 text-[1.15rem] font-bold" id="workspaces-title">
          {t("projectEdit.workspaces")}
        </h1>
        <button
          aria-label={t("projectEdit.attachWorkspace")}
          className="grid h-9 w-9 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-55"
          disabled={disabled}
          onClick={onAttach}
          type="button"
        >
          <Plus aria-hidden="true" size={20} strokeWidth={1.5} />
        </button>
      </div>
    </div>
  );
}

function ProjectWorkspaceList({
  defaultWorkspaceID,
  disabled,
  hasNextPage,
  header,
  isFetchingNextPage,
  onLoadMore,
  onMakeDefault,
  onUnlink,
  workspaces,
}: Readonly<{
  defaultWorkspaceID: string;
  disabled: boolean;
  hasNextPage: boolean;
  header: ReactNode;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
  onMakeDefault: (workspace: WorkspaceSummary) => void;
  onUnlink: (workspace: WorkspaceSummary) => void;
  workspaces: readonly WorkspaceSummary[];
}>) {
  const { t } = useTranslation();
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch]"
      empty={<p className="m-0 text-[var(--color-muted)]">{t("projectEdit.noWorkspaces")}</p>}
      estimateSize={() => 72}
      getItemKey={(workspace) => workspace.id}
      hasNextPage={hasNextPage}
      header={header}
      isFetchingNextPage={isFetchingNextPage}
      items={workspaces}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={onLoadMore}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(workspace) => (
        <div className={`mx-auto w-full ${projectEditContentMaxWidthClassName}`}>
          <WorkspaceRow
            defaultWorkspaceID={defaultWorkspaceID}
            disabled={disabled}
            onMakeDefault={() => {
              onMakeDefault(workspace);
            }}
            onUnlink={() => {
              onUnlink(workspace);
            }}
            workspace={workspace}
          />
        </div>
      )}
    />
  );
}

function workspaceUnlinkWindowOptions(target: WorkspaceUnlinkTarget, title: string) {
  return {
    initialHeight: 320,
    initialWidth: workspaceUnlinkDialogWidth,
    label: `workspace-unlink-${target.projectID}-${target.workspaceID}`,
    params: {
      projectID: target.projectID,
      rootPath: target.rootPath,
      workspaceID: target.workspaceID,
    },
    route: "/native-dialog/workspace-unlink",
    title,
  };
}
