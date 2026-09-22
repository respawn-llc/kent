import * as Atom from "effect/unstable/reactivity/Atom";
import * as Option from "effect/Option";
import { z } from "zod";
import type { QueryObserverResult } from "@tanstack/react-query";
import type { TaskDetail } from "@/api";
import type { QuerySnapshot } from "@/app-facade";
import type { TaskDraft } from "./TaskDetailRows";
import {
  initialDescriptionPresentationState,
  type DescriptionPresentationState,
} from "./TaskDetailDescriptionPresentation";

type TaskDraftState = Readonly<{ base: TaskDraft; draft: TaskDraft }>;
export type EditingComment = Readonly<{ id: string; body: string }> | null;

const retainedSchema = z.object({
  base: z.object({ body: z.string(), title: z.string() }),
  descriptionPresentation: z.object({ editing: z.boolean(), expanded: z.boolean() }),
  draft: z.object({ body: z.string(), title: z.string() }),
  editingComment: z.object({ body: z.string(), id: z.string() }).nullable(),
  newCommentBody: z.string(),
  selectedTab: z.enum(["comments", "activity"]),
});

export function createTaskDetailEditing(
  detail: Atom.Atom<QuerySnapshot<QueryObserverResult<TaskDetail>>>,
  retainedState: unknown,
) {
  const restored = retainedSchema.safeParse(retainedState).data;
  const drafts = Atom.writable<TaskDraftState | null, TaskDraft>(
    (get) => {
      const previous = Option.getOrNull(get.self<TaskDraftState | null>()) ?? restored ?? null;
      const task = get(detail).data;
      if (task === undefined) return previous;
      const server = { title: task.title, body: task.body };
      return previous === null ? { base: server, draft: server } : reconcileDraftState(previous, server);
    },
    (ctx, draft) => {
      const current = ctx.get(drafts);
      if (current === null) return;
      const task = ctx.get(detail).data;
      const next = { base: current.base, draft };
      ctx.setSelf(
        task === undefined ? next : reconcileDraftState(next, { title: task.title, body: task.body }),
      );
    },
  );
  const editingComment = Atom.make<EditingComment>(restored?.editingComment ?? null);
  const newCommentBody = Atom.make(restored?.newCommentBody ?? "");
  const descriptionPresentation = Atom.make<DescriptionPresentationState>(
    restored?.descriptionPresentation ?? initialDescriptionPresentationState,
  );
  const selectedTab = Atom.make<"comments" | "activity">(restored?.selectedTab ?? "comments");
  const dependencyFocus = Atom.make<number | null>(null);
  const retained = Atom.make((get) => ({
    ...get(drafts),
    editingComment: get(editingComment),
    newCommentBody: get(newCommentBody),
    descriptionPresentation: get(descriptionPresentation),
    selectedTab: get(selectedTab),
  }));
  const capture = Atom.make((get) => () => get.once(retained));
  const state = Atom.make((get) => {
    const values = get(retained);
    const task = get(detail).data;
    const draftState = get(drafts);
    const dirty = task !== undefined && draftState !== null && !sameTaskDraft(draftState.draft, task);
    return {
      ...values,
      drafts: draftState,
      dependencyFocus: get(dependencyFocus),
      dirty,
      canSave: dirty && draftState.draft.title.trim().length > 0,
    };
  });
  const view = {
    state,
    capture,
    editDraft: Atom.fn<TaskDraft>()((value) => Atom.set(drafts, value)),
    editComment: Atom.fn<EditingComment>()((value) => Atom.set(editingComment, value)),
    editNewComment: Atom.fn<string>()((value) => Atom.set(newCommentBody, value)),
    presentDescription: Atom.fn<DescriptionPresentationState>()((value) =>
      Atom.set(descriptionPresentation, value),
    ),
    selectTab: Atom.fn<"comments" | "activity">()((value) => Atom.set(selectedTab, value)),
    focusDependencies: Atom.fn(() =>
      Atom.update(dependencyFocus, (current) => (current === null ? 1 : current + 1)),
    ),
  } as const;
  return { drafts, editingComment, newCommentBody, view } as const;
}

export function sameTaskDraft(a: TaskDraft | undefined, b: TaskDraft | undefined): boolean {
  if (a === undefined || b === undefined) return a === b;
  return a.title === b.title && a.body === b.body;
}

function reconcileDraftState(state: TaskDraftState, serverDraft: TaskDraft): TaskDraftState {
  if (sameTaskDraft(state.draft, state.base)) {
    return sameTaskDraft(state.base, serverDraft) ? state : { base: serverDraft, draft: serverDraft };
  }
  if (sameTaskDraft(state.draft, serverDraft)) {
    return { base: serverDraft, draft: serverDraft };
  }
  return state;
}
