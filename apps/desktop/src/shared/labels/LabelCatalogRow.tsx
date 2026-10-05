import { useCallback, type Dispatch, type SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import type { ReorderableListItemRenderProps } from "@app/ui-kit";
import type { ProjectLabel } from "@/api";
import { useProjectLabelActions } from "./projectLabelHooks";
import { LabelRenameEditor, LabelResultRow, type DeleteState, type RenameState } from "./LabelChooserRows";
import {
  labelMutationErrorMessage,
  labelResultRowSelection,
  removeDeletedSelection,
  selectLabel,
} from "./labelChooserActions";
import type { LabelChooserInvocation } from "./LabelChooser";

export function LabelCatalogRow({
  label,
  rename,
  deletion,
  setRename,
  setDeletion,
  invocation,
  highlighted,
  sortable,
  reorderPending,
}: Readonly<{
  label: ProjectLabel;
  rename: RenameState | null;
  deletion: DeleteState | null;
  setRename: Dispatch<SetStateAction<RenameState | null>>;
  setDeletion: Dispatch<SetStateAction<DeleteState | null>>;
  invocation: LabelChooserInvocation;
  highlighted: boolean;
  sortable?: ReorderableListItemRenderProps | undefined;
  reorderPending: boolean;
}>) {
  const actions = useProjectLabelActions(label.id);
  const { t } = useTranslation();
  const attachRow = useCallback(
    (element: HTMLDivElement | null) => {
      sortable?.itemRef(element);
    },
    [sortable],
  );
  const selection = labelResultRowSelection(invocation, label.id);
  const row =
    rename?.labelID === label.id ? (
      <LabelRenameEditor
        rename={rename}
        pending={actions.rename.isPending}
        error={actions.rename.isError ? labelMutationErrorMessage(actions.rename.error, t) : null}
        onCancel={() => {
          setRename(null);
        }}
        onChange={(draft) => {
          setRename({ ...rename, draft });
          actions.rename.reset();
        }}
        onCommit={() => {
          actions.rename.submit({
            name: rename.draft,
            onSuccess: () => {
              setRename((latest) =>
                latest?.labelID === label.id && latest.draft === rename.draft ? null : latest,
              );
            },
          });
        }}
      />
    ) : (
      <LabelResultRow
        label={label}
        highlighted={highlighted}
        renamePending={actions.rename.isPending}
        deletePending={actions.delete.isPending}
        reorderPending={reorderPending}
        deletion={
          deletion?.labelID === label.id
            ? {
                ...deletion,
                pending: actions.delete.isPending,
                error: actions.delete.isError ? labelMutationErrorMessage(actions.delete.error, t) : null,
              }
            : null
        }
        onDeleteConfirm={() => {
          actions.delete.submit({
            onSuccess: () => {
              removeDeletedSelection(invocation, label.id);
              setDeletion((latest) => (latest?.labelID === label.id ? null : latest));
            },
          });
        }}
        onDeleteOpenChange={(open) => {
          setDeletion(open ? { labelID: label.id } : null);
        }}
        onRename={() => {
          actions.rename.reset();
          setRename({ labelID: label.id, draft: label.name });
        }}
        onSelect={() => {
          selectLabel(invocation, label.id, selection.kind === "binary" ? !selection.selected : true);
        }}
        reorder={sortable}
        selection={selection}
        selectionDisabled={invocation.kind === "assignment" && invocation.disabled === true}
      />
    );
  return sortable === undefined ? (
    row
  ) : (
    <div data-testid={`label-reorder-item-${label.id}`} ref={attachRow} style={sortable.style}>
      {row}
    </div>
  );
}
