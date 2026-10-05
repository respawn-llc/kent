import type { TFunction } from "i18next";
import { PlusIcon, SearchIcon } from "lucide-react";
import type { KeyboardEvent } from "react";
import {
  useId,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type ReactElement,
  type ReactNode,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";

import { ReorderableList, type ReorderableListItemRenderProps } from "@app/ui-kit";
import { workflowLabelMaxIDs, type ProjectLabel } from "@/api";
import { textFieldSubmitShortcutPolicyForPlatform, useAppServices, useStatusController } from "@/app-facade";
import {
  Button,
  IconTooltipButton,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SegmentedControl,
  Spinner,
  fieldInputClassName,
} from "@/ui";
import { UnlabeledResultRow, type DeleteState, type RenameState } from "./LabelChooserRows";
import type { LabelFilterAction, LabelFilterState } from "./labelFilterState";
import {
  handleLabelChooserSearchKeyDown,
  labelMutationErrorMessage,
  selectLabel,
  selectUnlabeled,
} from "./labelChooserActions";
import { useProjectLabelCatalog, useProjectLabelCatalogMutations } from "./projectLabelHooks";
import { LabelCatalogRow } from "./LabelCatalogRow";
import { LabelActionScopeContext } from "./projectLabelContext";

export type LabelChooserInvocation =
  | Readonly<{
      kind: "filter";
      state: LabelFilterState;
      onAction(action: LabelFilterAction): void;
    }>
  | Readonly<{
      disabled?: boolean | undefined;
      kind: "assignment";
      selectedLabelIDs: readonly string[];
      onCreatePendingChange?(pending: boolean): void;
      onSelectionChange(labelID: string, selected: boolean): void;
    }>;

export type LabelChooserProps = Readonly<{
  footer?: ReactNode;
  invocation: LabelChooserInvocation;
  onOpenChange?: ((open: boolean) => void) | undefined;
  open?: boolean | undefined;
  preferredSide?: "bottom" | "top" | undefined;
  trigger: ReactElement;
}>;

type LabelChooserChoice = Readonly<{ kind: "unlabeled" }> | Readonly<{ kind: "label"; label: ProjectLabel }>;

function prepareLabelComparison(value: string): string {
  return value.normalize("NFC").toUpperCase().normalize("NFC");
}

function labelNamesEqual(left: string, right: string): boolean {
  return prepareLabelComparison(left) === prepareLabelComparison(right);
}

function labelNameContains(name: string, query: string): boolean {
  return prepareLabelComparison(name).includes(prepareLabelComparison(query));
}

function canCreateLabel(name: string, labels: readonly ProjectLabel[]): boolean {
  return name.length > 0 && !labels.some((label) => labelNamesEqual(label.name, name));
}

function renderLabelChooserSearch({
  canCreate,
  catalogAtLimit,
  choiceCount,
  createError,
  catalogMutationPending,
  invocation,
  onCreate,
  onKeyDown,
  onSearchChange,
  preparedSearch,
  search,
  searchErrorID,
  t,
}: Readonly<{
  canCreate: boolean;
  catalogAtLimit: boolean;
  choiceCount: number;
  createError: string | null;
  catalogMutationPending: boolean;
  invocation: LabelChooserInvocation;
  onCreate(): void;
  onKeyDown(event: KeyboardEvent<HTMLInputElement>): void;
  onSearchChange(value: string): void;
  preparedSearch: string;
  search: string;
  searchErrorID: string;
  t: TFunction;
}>) {
  return (
    <div className="flex items-start gap-[var(--space-2)]">
      <div className="grid min-w-0 flex-1 gap-[var(--space-2)]">
        <span className="relative block">
          <SearchIcon
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-[var(--space-2)] -translate-y-1/2 text-[var(--color-muted)]"
            size={16}
            strokeWidth={1.8}
          />
          <input
            aria-describedby={createError === null ? undefined : searchErrorID}
            aria-invalid={createError === null ? undefined : true}
            aria-label={t("labels.search")}
            autoComplete="off"
            className={`${fieldInputClassName} text-sm`}
            onChange={(event) => {
              onSearchChange(event.currentTarget.value);
            }}
            onKeyDown={onKeyDown}
            inputMode="search"
            style={{
              height: "var(--space-6)",
              paddingBlock: "var(--space-0)",
              paddingInlineEnd: "calc(var(--space-6) + var(--space-1))",
              paddingInlineStart: "calc(var(--space-2) + var(--space-4) + var(--space-1))",
            }}
            type="text"
            value={search}
          />
          {canCreate ? (
            <span className="absolute top-1/2 right-[var(--space-1)] -translate-y-1/2">
              <IconTooltipButton
                disabled={catalogAtLimit}
                loading={catalogMutationPending}
                label={
                  catalogAtLimit ? t("labels.catalogLimit") : t("labels.create", { name: preparedSearch })
                }
                onClick={onCreate}
                size="icon-sm"
              >
                <PlusIcon aria-hidden="true" size={14} strokeWidth={1.8} />
              </IconTooltipButton>
            </span>
          ) : null}
        </span>
        {choiceCount === 0 && canCreate && !catalogAtLimit ? (
          <span className="px-[var(--space-2)] py-[var(--space-1)] text-sm leading-relaxed text-[var(--color-muted)]">
            {t("labels.noMatchesCreateHint")}
          </span>
        ) : null}
        {createError === null ? null : (
          <span
            className="px-[var(--space-1)] text-xs text-[var(--color-error)]"
            id={searchErrorID}
            role="alert"
          >
            {createError}
          </span>
        )}
      </div>
      {invocation.kind === "filter" ? (
        <SegmentedControl
          ariaLabel={t("labels.matchMode")}
          className="shrink-0"
          disabled={invocation.state.filter.kind === "unlabeled"}
          onValueChange={(mode) => {
            invocation.onAction({ type: "named.mode", mode });
          }}
          options={[
            { label: t("labels.matchAny"), value: "any" },
            { label: t("labels.matchAll"), value: "all" },
          ]}
          value={invocation.state.namedMode}
        />
      ) : null}
    </div>
  );
}

export function LabelChooser({
  footer,
  invocation,
  onOpenChange,
  open: controlledOpen,
  preferredSide,
  trigger,
}: LabelChooserProps) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const catalog = useProjectLabelCatalog();
  const mutations = useProjectLabelCatalogMutations();
  const [search, setSearch] = useState("");
  const [keyboardHighlightedIndex, setKeyboardHighlightedIndex] = useState<number | null>(null);
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false);
  const open = controlledOpen ?? uncontrolledOpen;
  const [rename, setRename] = useState<RenameState | null>(null);
  const [deletion, setDeletion] = useState<DeleteState | null>(null);
  const outsideInteractionRef = useRef(false);
  const searchErrorID = useId();
  const preparedSearch = search.trim().normalize("NFC");
  const labels = useMemo(
    () => catalog.data?.labels.filter((label) => labelNameContains(label.name, preparedSearch)) ?? [],
    [catalog.data, preparedSearch],
  );
  const canCreate = canCreateLabel(preparedSearch, labels);
  const catalogAtLimit = (catalog.data?.labels.length ?? 0) >= workflowLabelMaxIDs;
  const unlabeledName = t("labels.unlabeled");
  const showUnlabeledChoice =
    invocation.kind === "filter" &&
    (catalog.data?.labels.length ?? 0) > 0 &&
    labelNameContains(unlabeledName, preparedSearch);
  const choices: readonly LabelChooserChoice[] = [
    ...(showUnlabeledChoice ? ([{ kind: "unlabeled" }] as const) : []),
    ...labels.map((label) => ({ kind: "label" as const, label })),
  ];
  const choiceCount = choices.length;
  const createError = mutations.create.isError ? labelMutationErrorMessage(mutations.create.error, t) : null;
  const createLabel = () => {
    const notify = invocation.kind === "assignment" ? invocation.onCreatePendingChange : undefined;
    mutations.create.submit({
      name: preparedSearch,
      onStart: () => notify?.(true),
      onSettled: () => notify?.(false),
      onSuccess(label) {
        if (invocation.kind === "assignment") selectLabel(invocation, label.id, true);
        setSearch("");
        setKeyboardHighlightedIndex(null);
      },
    });
  };
  const reorderEnabled = invocation.kind === "filter" && preparedSearch.length === 0 && labels.length >= 2;
  return (
    <LabelActionScopeContext.Provider value={searchErrorID}>
      <Popover
        onOpenChange={(nextOpen) => {
          if (!nextOpen && (rename !== null || deletion !== null) && !outsideInteractionRef.current) {
            setRename(null);
            setDeletion(null);
            return;
          }
          outsideInteractionRef.current = false;
          if (controlledOpen === undefined) {
            setUncontrolledOpen(nextOpen);
          }
          onOpenChange?.(nextOpen);
          if (!nextOpen) {
            setSearch("");
            setKeyboardHighlightedIndex(null);
            setRename(null);
            setDeletion(null);
            mutations.create.reset();
          }
        }}
        open={open}
      >
        <PopoverTrigger asChild>{trigger}</PopoverTrigger>
        <PopoverContent
          align="start"
          className="max-h-[var(--radix-popover-content-available-height)] w-[min(25.3rem,calc(100vw-24px))] gap-[var(--space-2)] overflow-y-auto overscroll-contain p-[var(--space-2)]"
          collisionPadding={12}
          level={3}
          onEscapeKeyDown={(event) => {
            if (rename === null && deletion === null) {
              return;
            }
            event.preventDefault();
            setRename(null);
            setDeletion(null);
          }}
          onPointerDownOutside={() => {
            outsideInteractionRef.current = true;
          }}
          side={labelChooserPopoverSide(preferredSide)}
        >
          {renderLabelChooserSearch({
            canCreate,
            catalogAtLimit,
            choiceCount,
            createError,
            catalogMutationPending: mutations.create.isPending,
            invocation,
            onCreate() {
              createLabel();
            },
            onKeyDown(event) {
              handleLabelChooserSearchKeyDown({
                canCreate,
                catalogMutationPending: mutations.create.isPending,
                catalogAtLimit,
                createLabel,
                event,
                highlightedIndex: keyboardHighlightedIndex,
                invocation,
                policy: textFieldSubmitShortcutPolicyForPlatform(nativeBridge.capabilities.platform),
                choices,
                setHighlightedIndex: setKeyboardHighlightedIndex,
              });
            },
            onSearchChange(value) {
              setSearch(value);
              setKeyboardHighlightedIndex(null);
              mutations.create.reset();
            },
            preparedSearch,
            search,
            searchErrorID,
            t,
          })}
          {renderLabelChooserResults({
            catalog,
            choices,
            deletion,
            invocation,
            keyboardHighlightedIndex,
            rename,
            setDeletion,
            setKeyboardHighlightedIndex,
            setRename,
            t,
            unlabeledName,
            showUnlabeledChoice,
            labels,
            onReorder(nextLabels) {
              mutations.reorder.submit({
                labelIDs: nextLabels.map((label) => label.id),
                onError(error) {
                  push({
                    body: labelMutationErrorMessage(error, t),
                    durationMs: Infinity,
                    id: "project-label-reorder-error",
                    title: t("labels.mutationFailed"),
                    tone: "danger",
                  });
                },
              });
            },
            reorderEnabled,
            catalogMutationPending: mutations.reorder.isPending,
          })}
          {footer}
        </PopoverContent>
      </Popover>
    </LabelActionScopeContext.Provider>
  );
}

function labelChooserPopoverSide(side: LabelChooserProps["preferredSide"]): "bottom" | "top" {
  return side ?? "bottom";
}

function renderLabelChooserResults({
  catalog,
  choices,
  deletion,
  invocation,
  keyboardHighlightedIndex,
  rename,
  setDeletion,
  setKeyboardHighlightedIndex,
  setRename,
  t,
  unlabeledName,
  showUnlabeledChoice,
  labels,
  onReorder,
  reorderEnabled,
  catalogMutationPending,
}: Readonly<{
  catalog: ReturnType<typeof useProjectLabelCatalog>;
  choices: readonly LabelChooserChoice[];
  deletion: DeleteState | null;
  invocation: LabelChooserInvocation;
  keyboardHighlightedIndex: number | null;
  rename: RenameState | null;
  setDeletion: Dispatch<SetStateAction<DeleteState | null>>;
  setKeyboardHighlightedIndex: Dispatch<SetStateAction<number | null>>;
  setRename: Dispatch<SetStateAction<RenameState | null>>;
  t: TFunction;
  unlabeledName: string;
  showUnlabeledChoice: boolean;
  labels: readonly ProjectLabel[];
  onReorder(nextLabels: readonly ProjectLabel[]): void;
  reorderEnabled: boolean;
  catalogMutationPending: boolean;
}>) {
  const labelIndexes = new Map(labels.map((label, index) => [label.id, index]));
  if (catalog.isPending) {
    return (
      <div className="grid min-h-20 place-items-center" role="status">
        <Spinner />
      </div>
    );
  }
  if (catalog.isError) {
    return (
      <div className="grid gap-[var(--space-2)] p-[var(--space-2)] text-sm text-[var(--color-error)]">
        <span>{t("labels.loadFailed")}</span>
        <Button
          onClick={() => {
            catalog.refetch();
          }}
          variant="primary"
        >
          {t("app.retry")}
        </Button>
      </div>
    );
  }
  return (
    <div
      className="grid max-h-[min(calc(10*2.25rem+9*var(--space-1)),calc(var(--radix-popover-content-available-height)-10rem))] gap-[var(--space-1)] overflow-y-auto overscroll-contain pr-[var(--space-1)]"
      onPointerMove={() => {
        setKeyboardHighlightedIndex(null);
      }}
      role="list"
    >
      {reorderEnabled && showUnlabeledChoice ? (
        <UnlabeledResultRow
          highlighted={keyboardHighlightedIndex === 0}
          name={unlabeledName}
          onSelect={() => {
            selectUnlabeled(invocation);
          }}
          selected={invocation.kind === "filter" && invocation.state.filter.kind === "unlabeled"}
        />
      ) : null}
      {reorderEnabled ? (
        <ReorderableList
          disabled={catalogMutationPending}
          getItemID={(label) => label.id}
          items={labels}
          onCommit={({ items }) => {
            onReorder(items);
          }}
          renderItem={(label, sortable) =>
            renderLabelChooserChoiceRow({
              choice: { kind: "label", label },
              deletion,
              highlighted: (() => {
                const labelIndex = labelIndexes.get(label.id);
                return (
                  labelIndex !== undefined &&
                  keyboardHighlightedIndex === labelIndex + (showUnlabeledChoice ? 1 : 0)
                );
              })(),
              invocation,
              rename,
              setDeletion,
              setRename,
              unlabeledName,
              catalogMutationPending,
              sortable,
            })
          }
        />
      ) : (
        choices.map((choice, index) =>
          renderLabelChooserChoiceRow({
            choice,
            deletion,
            highlighted: index === keyboardHighlightedIndex,
            invocation,
            rename,
            setDeletion,
            setRename,
            unlabeledName,
            catalogMutationPending,
          }),
        )
      )}
    </div>
  );
}

function renderLabelChooserChoiceRow({
  choice,
  deletion,
  highlighted,
  invocation,
  rename,
  setDeletion,
  setRename,
  unlabeledName,
  catalogMutationPending,
  sortable,
}: Readonly<{
  choice: LabelChooserChoice;
  deletion: DeleteState | null;
  highlighted: boolean;
  invocation: LabelChooserInvocation;
  rename: RenameState | null;
  setDeletion: Dispatch<SetStateAction<DeleteState | null>>;
  setRename: Dispatch<SetStateAction<RenameState | null>>;
  unlabeledName: string;
  catalogMutationPending: boolean;
  sortable?: ReorderableListItemRenderProps | undefined;
}>) {
  if (choice.kind === "unlabeled") {
    return (
      <UnlabeledResultRow
        highlighted={highlighted}
        key="unlabeled"
        name={unlabeledName}
        onSelect={() => {
          selectUnlabeled(invocation);
        }}
        selected={invocation.kind === "filter" && invocation.state.filter.kind === "unlabeled"}
      />
    );
  }
  return (
    <LabelCatalogRow
      key={choice.label.id}
      label={choice.label}
      highlighted={highlighted}
      deletion={deletion}
      rename={rename}
      invocation={invocation}
      setDeletion={setDeletion}
      setRename={setRename}
      sortable={sortable}
      reorderPending={catalogMutationPending}
    />
  );
}
