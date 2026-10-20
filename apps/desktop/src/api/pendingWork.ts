import { UUIDv4Value } from "./setupOperationID";

export type PendingWorkItemID = UUIDv4Value<"pending_work_item">;
export type WorktreeOperationID = UUIDv4Value<"worktree_operation">;
export type PendingWorkIdentity = PendingWorkItemID | WorktreeOperationID;

export const pendingWorkItemIDFromProto = UUIDv4Value.fromProto<"pending_work_item">;
export const worktreeOperationIDFromProto = UUIDv4Value.fromProto<"worktree_operation">;

type PendingWorkItemFacts = Readonly<{
  state: "pending";
  canonicalInput: string;
}>;

export type PendingWorkItem =
  | (PendingWorkItemFacts &
      Readonly<{
        id: PendingWorkItemID;
        lane: "queue" | "steer";
        kind: "message";
        message: Readonly<{ text: string }>;
      }>)
  | (PendingWorkItemFacts &
      Readonly<{
        id: PendingWorkItemID;
        lane: "steer";
        kind: "manual_compaction";
        manualCompaction: Readonly<{ guidance: string | null }>;
      }>)
  | (PendingWorkItemFacts &
      Readonly<{
        id: WorktreeOperationID;
        lane: "steer";
        kind: "worktree_transition";
        worktreeTransition:
          Readonly<{ kind: "enter"; selector: string }> | Readonly<{ kind: "leave"; selector: null }>;
      }>);

export type PendingWork = Readonly<{ items: readonly PendingWorkItem[] }>;

export type PendingWorkRestoration = Readonly<{
  kind: PendingWorkItem["kind"];
  canonicalInput: string;
}>;
