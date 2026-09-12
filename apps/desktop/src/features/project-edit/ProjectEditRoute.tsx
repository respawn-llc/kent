import { useMemo, useState, type ReactElement, type ReactNode } from "react";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import { Plus, Save } from "lucide-react";

import type { ProjectEdit, WorkspaceCatalogRow } from "@/api";
import { errorMessage, isProjectMissingError } from "@/api";
import type { SidebarPageNavigator } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useAppNavigation } from "@/app-facade";
import { useNativeDialogFallback } from "@/app-facade";
import { usePublishSidebarHeaderAction } from "@/app-facade";
import { useSidebarBackWhen } from "@/app-facade";
import { useSidebarHeaderOffset } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { useTextFieldSubmitShortcut } from "@/app-facade";
import { useWindowChromeTitle } from "@/app-facade";
import {
  Button,
  ErrorState,
  HelpHint,
  LoadingState,
  VirtualizedInfiniteList,
  type VirtualizedInfiniteListBoundaryState,
} from "@/ui";
import { useQueryClient, type InfiniteQueryObserverResult } from "@tanstack/react-query";
import type { QuerySnapshot } from "@/app-facade";
import {
  createProjectEditViewModel,
  useProjectEditActions,
  type ProjectEditViewModel,
} from "./ProjectEditViewModel";
import { ProjectDeleteButton } from "./ProjectDeleteButton";
import {
  ProjectKeyField,
  ProjectNameField,
  WorkspaceRow,
  WorkspaceUnlinkFallbackDialog,
  type WorkspaceUnlinkTarget,
  workspaceUnlinkDialogWidth,
} from "./ProjectEditParts";

const projectEditContentMaxWidthClassName = "max-w-[1200px]";

export function ProjectEditRoute({
  navigator,
  projectId,
}: Readonly<{
  navigator?: SidebarPageNavigator;
  projectId: string;
}>): ReactElement | null {
  const { t } = useTranslation();
  const services = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const { openHome } = useAppNavigation();
  const [model] = useState(() =>
    createProjectEditViewModel({
      services,
      client,
      projectID: projectId,
      t,
      push,
      navigator,
    }),
  );
  useAtomMount(model.workspaceChanges);
  const query = useAtomValue(model.metadata);
  const actions = useProjectEditActions(model);
  const catalog = useAtomValue(model.catalog);
  const workspaceOccurrences = useAtomValue(model.workspaces);
  const projectMissing = [query.error, catalog.error].some(isProjectMissingError);
  useSidebarBackWhen(projectMissing, navigator);
  useWindowChromeTitle(query.data?.displayName ?? null);
  if (projectMissing && navigator !== undefined) return null;

  return (
    <ProjectEditContent
      model={model}
      catalogBoundary={projectCatalogBoundary(catalog, actions, t)}
      catalogPending={catalog.isPending}
      headerAccessory={
        navigator === undefined ? null : (
          <ProjectDeleteButton model={model} projectID={projectId} openHome={openHome} />
        )
      }
      hasNextPage={catalog.hasNextPage}
      hasPreviousPage={catalog.hasPreviousPage}
      isFetchingNextPage={catalog.isFetchingNextPage}
      isFetchingPreviousPage={catalog.isFetchingPreviousPage}
      key={query.data?.projectID ?? projectId}
      metadata={
        query.isPending
          ? { state: "pending" }
          : query.isError
            ? {
                state: "error",
                error: query.error,
                onRetry: () => {
                  actions.retryMetadata();
                },
              }
            : { state: "loaded", project: query.data }
      }
      onLoadMore={() => {
        actions.nextPage();
      }}
      onLoadPrevious={() => {
        actions.previousPage();
      }}
      previousBoundary={projectCatalogPreviousBoundary(catalog, actions.previousPage, t)}
      projectID={projectId}
      workspaceOccurrences={workspaceOccurrences}
    />
  );
}

function ProjectEditContent({
  model,
  catalogBoundary,
  catalogPending,
  headerAccessory,
  hasNextPage,
  hasPreviousPage,
  isFetchingNextPage,
  isFetchingPreviousPage,
  metadata,
  onLoadMore,
  onLoadPrevious,
  previousBoundary,
  projectID,
  workspaceOccurrences,
}: Readonly<{
  model: ProjectEditViewModel;
  catalogBoundary: VirtualizedInfiniteListBoundaryState | undefined;
  catalogPending: boolean;
  headerAccessory?: ReactNode;
  hasNextPage: boolean;
  hasPreviousPage: boolean;
  isFetchingNextPage: boolean;
  isFetchingPreviousPage: boolean;
  metadata: ProjectEditMetadataState;
  onLoadMore: () => void;
  onLoadPrevious: () => void;
  previousBoundary: VirtualizedInfiniteListBoundaryState | undefined;
  projectID: string;
  workspaceOccurrences: readonly ProjectWorkspaceOccurrence[];
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const {
    save: saveProject,
    editName: setNameDraft,
    editKey: setKeyDraft,
    makeDefault: saveDefaultWorkspace,
    chooseWorkspace,
    unlink,
  } = useProjectEditActions(model);
  const {
    keyDraft,
    nameDraft,
    nameErrors,
    keyErrors,
    dirty,
    canSave: validSave,
    pending,
  } = useAtomValue(model.state);
  const mutating = pending;
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
        onConfirm={(nextTarget) => {
          unlink({ workspaceID: nextTarget.workspaceID, close });
        }}
        target={target}
      />
    ),
  });
  // Publish the save control into the shared sidebar header (left of delete). It only appears when a
  // draft differs from the saved value; the ViewModel owns action admission.
  const canSave = validSave && !mutating;
  const projectSaveShortcut = useTextFieldSubmitShortcut({
    action: () => {
      saveProject(undefined);
    },
    available: canSave,
    kind: "direct",
  });
  const headerSaveAction = useMemo<ReactNode>(() => {
    if (!dirty) {
      return null;
    }
    return (
      <Button
        aria-label={t("projectEdit.saveName")}
        disabled={!canSave}
        onClick={() => {
          saveProject(undefined);
        }}
        size="icon"
        title={t("projectEdit.saveName")}
        variant="primary"
      >
        <Save aria-hidden="true" size={18} strokeWidth={1.5} />
      </Button>
    );
  }, [canSave, dirty, saveProject, t]);
  const headerActions = useMemo(
    () => (
      <>
        {headerSaveAction}
        {headerAccessory}
      </>
    ),
    [headerAccessory, headerSaveAction],
  );
  usePublishSidebarHeaderAction(headerActions);

  const header = (
    <ProjectEditListHeader
      disabled={mutating}
      keyDraft={keyDraft}
      keyErrors={keyErrors}
      metadata={metadata}
      nameDraft={nameDraft}
      nameErrors={nameErrors}
      onAttach={() => {
        chooseWorkspace(undefined);
      }}
      onKeyDown={projectSaveShortcut}
      onKeyChange={setKeyDraft}
      onNameChange={setNameDraft}
    />
  );

  return (
    <section
      aria-labelledby="workspaces-title"
      className="h-full min-h-0 overflow-hidden"
      data-testid="project-edit-route"
    >
      {unlinkDialog.fallback}
      <ProjectWorkspaceList
        disabled={mutating}
        hasNextPage={hasNextPage}
        hasPreviousPage={hasPreviousPage}
        header={header}
        isFetchingNextPage={isFetchingNextPage}
        isFetchingPreviousPage={isFetchingPreviousPage}
        nextBoundary={catalogBoundary}
        onLoadMore={onLoadMore}
        onLoadPrevious={onLoadPrevious}
        previousBoundary={previousBoundary}
        onMakeDefault={(workspace) => {
          saveDefaultWorkspace(workspace);
        }}
        onUnlink={(workspace) => {
          void unlinkDialog.open({
            projectID,
            rootPath: workspace.rootPath,
            workspaceID: workspace.id,
          });
        }}
        catalogPending={catalogPending}
        workspaceOccurrences={workspaceOccurrences}
      />
    </section>
  );
}

function ProjectEditListHeader({
  disabled,
  keyDraft,
  keyErrors,
  metadata,
  nameDraft,
  nameErrors,
  onAttach,
  onKeyDown,
  onKeyChange,
  onNameChange,
}: Readonly<{
  disabled: boolean;
  keyDraft: string;
  keyErrors: readonly string[];
  metadata: ProjectEditMetadataState;
  nameDraft: string;
  nameErrors: readonly string[];
  onAttach: () => void;
  onKeyDown: React.KeyboardEventHandler<HTMLInputElement>;
  onKeyChange: (value: string) => void;
  onNameChange: (value: string) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className={`mx-auto grid w-full ${projectEditContentMaxWidthClassName} gap-[var(--space-3)]`}>
      <div className="grid min-w-0 gap-[var(--space-3)]">
        {metadata.state === "pending" ? (
          <LoadingState body={t("states.loading")} reveal={false} title={t("projectEdit.loadingTitle")} />
        ) : null}
        {metadata.state === "error" ? (
          <ErrorState
            body={errorMessage(metadata.error)}
            onRetry={metadata.onRetry}
            reveal={false}
            retryLabel={t("app.retry")}
            title={t("states.error")}
          />
        ) : null}
        {metadata.state === "loaded" ? (
          <>
            <ProjectNameField
              disabled={disabled}
              nameDraft={nameDraft}
              nameErrors={nameErrors}
              onKeyDown={onKeyDown}
              onNameChange={onNameChange}
            />
            <ProjectKeyField
              disabled={disabled}
              keyDraft={keyDraft}
              keyErrors={keyErrors}
              onKeyDown={onKeyDown}
              onKeyChange={onKeyChange}
            />
          </>
        ) : null}
      </div>
      <div className="flex min-w-0 items-center justify-between gap-[var(--space-3)]">
        <span className="inline-flex min-w-0 items-center gap-[var(--space-1)]">
          <h1 className="m-0 truncate text-[1.15rem] font-bold" id="workspaces-title">
            {t("projectEdit.workspaces")}
          </h1>
          <HelpHint className="shrink-0" label={t("projectEdit.workspacesHelp")} side="bottom" />
        </span>
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
  catalogPending,
  disabled,
  hasNextPage,
  hasPreviousPage,
  header,
  isFetchingNextPage,
  isFetchingPreviousPage,
  nextBoundary,
  onLoadMore,
  onLoadPrevious,
  onMakeDefault,
  onUnlink,
  workspaceOccurrences,
  previousBoundary,
}: Readonly<{
  catalogPending: boolean;
  disabled: boolean;
  hasNextPage: boolean;
  hasPreviousPage: boolean;
  header: ReactNode;
  isFetchingNextPage: boolean;
  isFetchingPreviousPage: boolean;
  nextBoundary: VirtualizedInfiniteListBoundaryState | undefined;
  onLoadMore: () => void;
  onLoadPrevious: () => void;
  onMakeDefault: (workspace: WorkspaceCatalogRow) => void;
  onUnlink: (workspace: WorkspaceCatalogRow) => void;
  workspaceOccurrences: readonly ProjectWorkspaceOccurrence[];
  previousBoundary: VirtualizedInfiniteListBoundaryState | undefined;
}>) {
  const { t } = useTranslation();
  const headerOffset = useSidebarHeaderOffset();
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch]"
      empty={
        catalogPending ? (
          <LoadingState body={t("states.loading")} reveal={false} title={t("projectEdit.workspaces")} />
        ) : nextBoundary?.state === "error" ? (
          <ErrorState
            body={nextBoundary.message}
            onRetry={nextBoundary.onRetry}
            reveal={false}
            retryLabel={nextBoundary.retryLabel}
            title={t("projectEdit.workspaces")}
          />
        ) : (
          <p className="m-0 text-[var(--color-muted)]">{t("projectEdit.noWorkspaces")}</p>
        )
      }
      estimateSize={() => 72}
      getItemKey={(occurrence) => occurrence.occurrenceKey}
      hasNextPage={hasNextPage && nextBoundary?.state !== "error"}
      hasPreviousPage={hasPreviousPage && previousBoundary?.state !== "error"}
      header={header}
      isFetchingNextPage={isFetchingNextPage}
      isFetchingPreviousPage={isFetchingPreviousPage}
      items={workspaceOccurrences}
      loadingLabel={t("app.loadingMore")}
      nextBoundary={workspaceOccurrences.length === 0 ? undefined : nextBoundary}
      onLoadMore={onLoadMore}
      onLoadPrevious={onLoadPrevious}
      previousBoundary={previousBoundary}
      previousLoadItemKey={workspaceOccurrences[0]?.occurrenceKey}
      paddingEnd={16}
      paddingStart={16 + headerOffset}
      renderItem={({ workspace }) => (
        <div className={`mx-auto w-full ${projectEditContentMaxWidthClassName}`}>
          <WorkspaceRow
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

type ProjectEditMetadataState =
  | Readonly<{ state: "pending" }>
  | Readonly<{ state: "error"; error: unknown; onRetry: () => void }>
  | Readonly<{ state: "loaded"; project: ProjectEdit }>;

type ProjectWorkspaceOccurrence = Readonly<{
  occurrenceKey: string;
  workspace: WorkspaceCatalogRow;
}>;

function projectCatalogBoundary(
  catalog: QuerySnapshot<InfiniteQueryObserverResult>,
  actions: ReturnType<typeof useProjectEditActions>,
  t: ReturnType<typeof useTranslation>["t"],
): VirtualizedInfiniteListBoundaryState | undefined {
  if (catalog.isFetchingNextPage) {
    return { state: "loading", label: t("app.loadingMore") };
  }
  if (catalog.isFetchNextPageError || (catalog.isError && catalog.data === undefined)) {
    return {
      state: "error",
      message: errorMessage(catalog.error),
      retryLabel: t("app.retry"),
      onRetry: () => {
        if (catalog.data === undefined) {
          actions.retryCatalog();
        } else {
          actions.nextPage();
        }
      },
    };
  }
  return undefined;
}

function projectCatalogPreviousBoundary(
  catalog: QuerySnapshot<InfiniteQueryObserverResult>,
  previousPage: () => void,
  t: ReturnType<typeof useTranslation>["t"],
): VirtualizedInfiniteListBoundaryState | undefined {
  if (catalog.isFetchingPreviousPage) {
    return { state: "loading", label: t("app.loadingMore") };
  }
  if (catalog.isFetchPreviousPageError) {
    return {
      state: "error",
      message: errorMessage(catalog.error),
      retryLabel: t("app.retry"),
      onRetry: () => {
        previousPage();
      },
    };
  }
  return undefined;
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
