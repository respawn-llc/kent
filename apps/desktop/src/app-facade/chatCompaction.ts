import type { ChatRuntimeActivity } from "@/api";

export function isCompactionKind(kind: NonNullable<ChatRuntimeActivity["activeStep"]>["activeKind"]) {
  return kind === "compaction" || kind === "pre_submit_compaction";
}

export function isCompacting(activity: ChatRuntimeActivity | null | undefined): boolean {
  return activity?.activeStep != null && isCompactionKind(activity.activeStep.activeKind);
}
