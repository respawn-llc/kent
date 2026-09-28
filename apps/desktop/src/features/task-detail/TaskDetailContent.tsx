import { useAtomValue } from "@effect/atom-react";

import type { TaskDependencyDirection, TaskDetail } from "@/api";
import type {
  SidebarPageNavigator,
  SidebarMode,
  SidebarRootController,
  SidebarDestination,
  TaskDetailInitialFocus,
} from "@/app-facade";
import { useAppNavigation } from "@/app-facade";
import { TaskDeleteProvider } from "./TaskDeleteButton";
import { TaskDetailList } from "./TaskDetailList";
import type { TaskDetailSessionChatEntry } from "./taskDetailSessionChat";
import { taskDetailSidebarDestination } from "./taskDetailSidebarDestination";
import type { TaskDetailDeleteDismissal } from "./taskDetailDismissal";
import type { PromptAnswerKey } from "./PromptAnswerState";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";
import { TaskInitiatingActionProvider } from "./TaskResumeButton";
import type { TaskDraft } from "./TaskDetailRows";
import type { TaskDetailReads, TaskDetailViewModel } from "./TaskDetailViewModel";
import type { useTaskDetailActions } from "./TaskDetailViewModel";

export function TaskDetailContent({
  activity,
  attention,
  comments,
  detail,
  model,
  actions,
  initialFocus,
  onDeleteDismiss,
  onMutated,
  openSessionChat,
  navigator,
  sidebarDestination,
  sidebarMode,
  openSidebar,
}: Readonly<{
  activity: TaskDetailReads["activity"];
  attention: TaskDetailReads["attention"];
  comments: TaskDetailReads["comments"];
  detail: TaskDetail;
  model: TaskDetailViewModel;
  actions: ReturnType<typeof useTaskDetailActions>;
  initialFocus?: TaskDetailInitialFocus | undefined;
  onDeleteDismiss: TaskDetailDeleteDismissal;
  onMutated?: (() => void) | undefined;
  openSessionChat?: TaskDetailSessionChatEntry | undefined;
  navigator?: SidebarPageNavigator | undefined;
  openSidebar?: SidebarRootController["open"] | undefined;
  sidebarDestination?: Extract<SidebarDestination, { kind: "taskDetail" }> | undefined;
  sidebarMode?: SidebarMode | undefined;
}>) {
  const navigation = useAppNavigation();
  const relationshipNavigationAvailable = hasRelationshipNavigation(navigator, openSidebar);
  const editing = useAtomValue(model.editing.state);
  const {
    drafts: draftState,
    editingComment,
    newCommentBody,
    descriptionPresentation,
    selectedTab,
    dependencyFocus: localDependencyFocusRequest,
  } = editing;
  const promptAnswerState = useAtomValue(model.prompts.state);
  const primaryFocusRequest = useAtomValue(model.prompts.focus);
  const mutations = {
    interruptPending: actions.interruptRequest.isPending,
    approvalPending: actions.approvalRequest.isPending,
    interrupt: () => {
      actions.interrupt({ onChanged: onMutated });
    },
    approve: (approvalID: string) => {
      actions.approve({ approvalID, onChanged: onMutated });
    },
  };
  if (draftState === null) throw new Error("Loaded Task requires an editing baseline");
  const draft = draftState.draft;
  const focusPresentation = taskDetailFocusPresentation({
    initialFocus,
    localDependencyFocusRequest,
  });

  function saveDraft(nextDraft: TaskDraft = draft, onSaved?: () => void): void {
    actions.save({ draft: nextDraft, onChanged: onMutated, ...(onSaved === undefined ? {} : { onSaved }) });
  }

  return (
    <TaskInitiatingActionProvider
      onApplied={async () => model.lifecycle.refresh({ projectID: detail.projectID, onChanged: onMutated })}
      onViewDependencies={(taskID) => {
        presentTaskDependencies({
          navigator,
          openSidebar,
          requestLocalFocus: () => {
            actions.focusDependencies();
          },
          sidebarDestination,
          taskID,
        });
      }}
      taskID={detail.id}
    >
      <TaskDeleteProvider
        pending={actions.deletion.isPending}
        onDelete={() => {
          actions.remove({ dismiss: onDeleteDismiss });
        }}
      >
        <TaskDetailList
          activity={activity}
          answerQuestion={{
            submit: (input, attempt) => {
              actions.answer({ input, ...attempt });
            },
          }}
          attention={attention}
          comments={comments}
          commentActions={{
            pending: actions.commentRequest.isPending,
            editingPending:
              actions.commentRequest.isPending && actions.commentRequest.variables.kind !== "delete",
            submit: () => {
              actions.submitComment({ onChanged: onMutated });
            },
            remove: (id) => {
              actions.deleteComment({ id, onChanged: onMutated });
            },
          }}
          detail={detail}
          draft={draft}
          draftDirty={editing.dirty}
          canSaveDraft={editing.canSave}
          descriptionPresentation={descriptionPresentation}
          editingComment={editingComment}
          focusRequestKey={dependencyFocusRequestKey(detail.id, localDependencyFocusRequest)}
          initialFocus={focusPresentation}
          mutations={mutations}
          newCommentBody={newCommentBody}
          relationshipNavigationAvailable={relationshipNavigationAvailable}
          onDraftChange={actions.editDraft}
          onDescriptionPresentationChange={actions.presentDescription}
          onAddDependency={(direction) => {
            openRelatedTaskCreation({ detail, direction, navigator, openSidebar });
          }}
          onDependenciesChanged={onMutated}
          onSelectDependencyTask={(taskID) => {
            if (navigator !== undefined) {
              navigator.push(
                sidebarDestination === undefined
                  ? dependencyDestination(taskID, sidebarMode)
                  : taskDetailSidebarDestination(sidebarDestination, taskID),
              );
              return;
            }
            void navigation.replaceTask(taskID);
          }}
          onNewCommentBodyChange={actions.editNewComment}
          onEditingCommentChange={actions.editComment}
          onQuestionSelectionChange={(key: PromptAnswerKey, selection: QuestionSelectionState) => {
            actions.editSelection({ key, selection });
          }}
          onSaveDraft={saveDraft}
          openSessionChat={openSessionChat}
          primaryFocusRequest={primaryFocusRequest}
          promptAnswerState={promptAnswerState}
          selectedTab={selectedTab}
          setTab={actions.selectTab}
          updateError={actions.update.error}
          updatePending={actions.update.isPending}
        />
      </TaskDeleteProvider>
    </TaskInitiatingActionProvider>
  );
}

function taskDetailFocusPresentation({
  initialFocus,
  localDependencyFocusRequest,
}: Readonly<{
  initialFocus: TaskDetailInitialFocus | undefined;
  localDependencyFocusRequest: number | null;
}>): TaskDetailInitialFocus | undefined {
  if (localDependencyFocusRequest !== null) {
    return { kind: "dependencies" };
  }
  return initialFocus;
}

function presentTaskDependencies({
  navigator,
  openSidebar,
  requestLocalFocus,
  sidebarDestination,
  taskID,
}: Readonly<{
  navigator: SidebarPageNavigator | undefined;
  openSidebar: SidebarRootController["open"] | undefined;
  requestLocalFocus(): void;
  sidebarDestination: Extract<SidebarDestination, { kind: "taskDetail" }> | undefined;
  taskID: string;
}>): void {
  if (navigator !== undefined && sidebarDestination !== undefined) {
    navigator.replace(taskDetailSidebarDestination(sidebarDestination, taskID, { kind: "dependencies" }));
    return;
  }
  if (openSidebar !== undefined) {
    openSidebar({
      kind: "taskDetail",
      initialFocus: { kind: "dependencies" },
      taskID,
    });
    return;
  }
  requestLocalFocus();
}

function dependencyDestination(
  taskID: string,
  mode: SidebarMode | undefined,
): Extract<SidebarDestination, { kind: "taskDetail" }> {
  return {
    kind: "taskDetail",
    taskID,
    ...(mode === undefined ? {} : { mode }),
  };
}

function openRelatedTaskCreation({
  detail,
  direction,
  navigator,
  openSidebar,
}: Readonly<{
  detail: TaskDetail;
  direction: TaskDependencyDirection;
  navigator?: SidebarPageNavigator | undefined;
  openSidebar?: SidebarRootController["open"] | undefined;
}>) {
  const destination = {
    boardQueryWorkflowID: detail.workflowID,
    initialSourceWorkspaceID: detail.sourceWorkspace.id,
    initialPreparedDependency: {
      direction: direction === "blocked-by" ? ("blocks" as const) : ("blocked-by" as const),
      taskID: detail.id,
      shortID: detail.shortID,
      title: detail.title,
      workflowID: detail.workflowID,
      status: detail.status,
    },
    kind: "newTask" as const,
    mode: "overlay" as const,
    projectID: detail.projectID,
    workflowID: detail.workflowID,
  };
  if (navigator !== undefined) {
    navigator.push(destination);
    return;
  }
  openSidebar?.(destination);
}

function hasRelationshipNavigation(
  navigator: SidebarPageNavigator | undefined,
  openSidebar: SidebarRootController["open"] | undefined,
): boolean {
  return navigator !== undefined || openSidebar !== undefined;
}

function dependencyFocusRequestKey(taskID: string, request: number | null): string | undefined {
  return request === null ? undefined : `${taskID}:dependencies:${request.toString()}`;
}
