import { createContext, useContext, useLayoutEffect, useRef, type ReactNode } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useSpeechField } from "@/shared/speech-input";
import type { TaskDetailViewModel, useTaskDetailActions } from "./TaskDetailViewModel";
import { promptAnswerKey, samePromptAnswerKey, type PromptAnswerKey } from "./PromptAnswerState";
import { emptyQuestionSelection, withQuestionCommentary } from "./TaskDetailQuestionState";

export function useTaskDetailSpeech(
  model: TaskDetailViewModel,
  actions: ReturnType<typeof useTaskDetailActions>,
) {
  const editing = useAtomValue(model.editing.state);
  const prompts = useAtomValue(model.prompts.state);
  const attention = useAtomValue(model.attention);
  const promptOwner = useRef({ prompts, attention });
  useLayoutEffect(() => {
    promptOwner.current = { prompts, attention };
  });
  const owner = useRef(editing);
  useLayoutEffect(() => {
    owner.current = editing;
  });
  const descriptionEditor = useRef<HTMLTextAreaElement>(null);
  const commentEditor = useRef<HTMLTextAreaElement>(null);
  const description = useSpeechField({
    value: editing.drafts?.draft.body ?? "",
    onChange: (body) => {
      const draft = owner.current.drafts?.draft;
      if (draft !== undefined) actions.editDraft({ ...draft, body });
    },
    readOnly: editing.drafts === null,
    editorRef: descriptionEditor,
    captureDraft: () => ({
      read: () => owner.current.drafts?.draft.body ?? null,
      write: (body) => {
        const draft = owner.current.drafts?.draft;
        if (draft !== undefined) actions.editDraft({ ...draft, body });
      },
      editor: () => descriptionEditor.current,
    }),
  });
  const comment = useSpeechField({
    value: editing.editingComment?.body ?? editing.newCommentBody,
    onChange: (body) => {
      const current = owner.current.editingComment;
      if (current === null) actions.editNewComment(body);
      else actions.editComment({ id: current.id, body });
    },
    readOnly: actions.commentRequest.isPending,
    editorRef: commentEditor,
    captureDraft: () => {
      const original = owner.current.editingComment;
      if (original === null)
        return {
          read: () => owner.current.newCommentBody,
          write: (text) => {
            actions.editNewComment(text);
          },
          editor: () => (owner.current.editingComment === null ? commentEditor.current : null),
        };
      return {
        read: () =>
          owner.current.editingComment?.lifetime === original.lifetime
            ? owner.current.editingComment.body
            : null,
        write: (body) => {
          actions.editComment({ id: original.id, body });
        },
        editor: () =>
          owner.current.editingComment?.lifetime === original.lifetime ? commentEditor.current : null,
      };
    },
  });
  const questionDraft = (key: PromptAnswerKey) => {
    const selection = () => {
      const current = promptOwner.current;
      if (
        current.prompts.isMasked(key) ||
        !current.attention.data?.items.some(
          (item) => item.kind === "question" && samePromptAnswerKey(promptAnswerKey(item), key),
        )
      )
        return null;
      return current.prompts.selection(key) ?? emptyQuestionSelection();
    };
    return {
      read: () => selection()?.answer ?? null,
      write: (text: string) => {
        const current = selection();
        if (current !== null)
          actions.editSelection({ key, selection: withQuestionCommentary(current, text) });
      },
    };
  };
  return { description, comment, questionDraft };
}

type TaskSpeech = ReturnType<typeof useTaskDetailSpeech>;
const Context = createContext<TaskSpeech | null>(null);
export function TaskSpeechProvider({
  value,
  children,
}: Readonly<{ value: TaskSpeech; children: ReactNode }>) {
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useTaskSpeech(): TaskSpeech {
  const value = useContext(Context);
  if (value === null) throw new Error("Task speech destination is required.");
  return value;
}
