import { useState } from "react";

import type { TaskDetail } from "../../api";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useUpdateTask } from "../tasks/useTaskMutations";
import { TaskDetailList } from "./TaskDetailList";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";
import type { TaskDraft } from "./TaskDetailRows";
import { useTaskMutations } from "./useTaskDetailData";
import type { useTaskActivity, useTaskComments } from "./useTaskDetailData";

type TaskDraftState = Readonly<{
  sourceKey: string;
  draft: TaskDraft;
}>;

export function TaskDetailContent({
  activity,
  comments,
  detail,
  initialFocus,
  onMutated,
  openLink,
  resumeRunId,
}: Readonly<{
  activity: ReturnType<typeof useTaskActivity>;
  comments: ReturnType<typeof useTaskComments>;
  detail: TaskDetail;
  initialFocus?: "firstQuestion" | undefined;
  onMutated?: (() => void) | undefined;
  openLink: (url: string) => void;
  resumeRunId: string;
}>) {
  const draftSourceKey = taskDraftSourceKey(detail);
  const [draftState, setDraftState] = useState<TaskDraftState>(() => ({
    sourceKey: draftSourceKey,
    draft: taskDraft(detail),
  }));
  const [editingComment, setEditingComment] = useState<Readonly<{ id: string; body: string }> | null>(null);
  const [newCommentBody, setNewCommentBody] = useState("");
  const [selectedTab, setSelectedTab] = useState<"comments" | "activity">("comments");
  const [questionSelections, setQuestionSelections] = useState<ReadonlyMap<string, QuestionSelectionState>>(
    () => new Map(),
  );
  const update = useUpdateTask(detail.id);
  const mutations = useTaskMutations(detail.id, onMutated);
  const connection = useConnectionSnapshot();
  const draft = draftState.sourceKey === draftSourceKey ? draftState.draft : taskDraft(detail);

  async function saveDraft(nextDraft: TaskDraft = draft): Promise<void> {
    await update.mutateAsync({
      taskID: detail.id,
      title: nextDraft.title,
      body: nextDraft.body,
    });
    onMutated?.();
  }

  return (
    <TaskDetailList
      activity={activity}
      comments={comments}
      detail={detail}
      disabled={connection.phase !== "connected"}
      draft={draft}
      editingComment={editingComment}
      initialFocus={initialFocus}
      mutations={mutations}
      newCommentBody={newCommentBody}
      onDraftChange={(nextDraft) => {
        setDraftState({ sourceKey: draftSourceKey, draft: nextDraft });
      }}
      onNewCommentBodyChange={setNewCommentBody}
      onEditingCommentChange={setEditingComment}
      onQuestionSelectionChange={(askID, selection) => {
        setQuestionSelections((previous) => new Map(previous).set(askID, selection));
      }}
      onSaveDraft={saveDraft}
      openLink={openLink}
      questionSelections={questionSelections}
      resumeRunId={resumeRunId}
      selectedTab={selectedTab}
      setTab={setSelectedTab}
      updateError={update.error}
      updatePending={update.isPending}
    />
  );
}

function taskDraft(detail: TaskDetail): TaskDraft {
  return { title: detail.title, body: detail.body };
}

function taskDraftSourceKey(detail: TaskDetail): string {
  return `${detail.id}:${detail.updatedAt.toString()}`;
}
