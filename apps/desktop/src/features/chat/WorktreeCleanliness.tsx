import { useTranslation } from "react-i18next";
import { DirtyStateKind, type WorktreeDeletePreview } from "@/api";

export function WorktreeCleanliness({
  value,
}: Readonly<{ value: NonNullable<WorktreeDeletePreview["cleanliness"]> }>) {
  const { t } = useTranslation();
  const clean = value.kind === DirtyStateKind.DIRTY_STATE_CLEAN;
  const label = clean
    ? t("chat.worktree.clean")
    : value.kind === DirtyStateKind.DIRTY_STATE_DIRTY
      ? t("chat.worktree.dirty", { count: value.dirtyFileCount })
      : t("chat.worktree.unknown", { diagnostic: value.unknownCause });
  return (
    <p className={clean ? "break-words text-sm" : "break-words text-sm text-[var(--color-warning)]"}>
      {label}
    </p>
  );
}
