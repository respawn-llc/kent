import { Link2, Pencil, Pin, PinOff, Plus } from "lucide-react";
import { useEffect, useRef, useState, type FocusEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { WorkflowBoard, WorkflowPickerItem } from "../../api";
import { Item, ItemContent, ItemGroup, ItemTitle } from "../../ui";
import { cx } from "../../ui/classes";

const collapseDelayMs = 500;

export type BoardHoverMenuProps = Readonly<{
    board: WorkflowBoard;
    canCreateTask: boolean;
    onNewTask: () => void;
    onWorkflowEdit: (workflowID: string) => void;
    onWorkflowLink: () => void;
    onWorkflowSelect: (workflowID: string) => void;
}>;

export function BoardHoverMenu({ board, canCreateTask, onNewTask, onWorkflowEdit, onWorkflowLink, onWorkflowSelect }: BoardHoverMenuProps) {
    const { t } = useTranslation();
    const menuRef = useRef<HTMLDivElement | null>(null);
    const collapseTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [hovered, setHovered] = useState(false);
    const [focused, setFocused] = useState(false);
    const [pinned, setPinned] = useState(false);
    const expanded = hovered || focused || pinned;

    useEffect(
        () => () => {
            clearCollapseTimer(collapseTimer);
        },
        [],
    );

    function expandNow(): void {
        clearCollapseTimer(collapseTimer);
        setHovered(true);
    }

    function collapseSoon(): void {
        clearCollapseTimer(collapseTimer);
        if (pinned) {
            setHovered(false);
            return;
        }
        collapseTimer.current = setTimeout(() => {
            setHovered(false);
        }, collapseDelayMs);
    }

    function closeWhenFocusLeaves(event: FocusEvent<HTMLDivElement>): void {
        if (event.relatedTarget instanceof Node && menuRef.current?.contains(event.relatedTarget)) {
            return;
        }
        setFocused(false);
    }

    return (
        <div
            className={cx(
                "board-hover-menu-morph island-glass app-region-no-drag fixed bottom-[var(--space-4)] left-[var(--space-4)] z-50 grid min-h-[var(--board-menu-collapsed-height)] max-h-[min(700px,70vh)] overflow-hidden rounded-[var(--radius-l)] border p-[var(--board-menu-padding)] shadow-[var(--shadow-island-1)]",
                expanded
                    ? "board-hover-menu-expanded grid-rows-[1fr] w-[min(360px,calc(100vw-32px))]"
                    : "board-hover-menu-collapsed grid-rows-[0fr] w-[var(--board-menu-collapsed-width)]",
            )}
            onBlur={closeWhenFocusLeaves}
            onFocus={() => {
                clearCollapseTimer(collapseTimer);
                setFocused(true);
            }}
            onMouseEnter={expandNow}
            onMouseLeave={collapseSoon}
            ref={menuRef}
            role="navigation"
        >
            <div className="min-h-0 overflow-hidden">
                <div
                    aria-hidden={!expanded}
                    className={cx(
                        "board-hover-menu-content grid max-h-[calc(min(700px,70vh)-var(--board-menu-collapsed-height))] min-h-0 min-w-0 gap-[var(--board-menu-content-gap)] overflow-y-auto pr-[var(--board-menu-content-inline-padding)] pb-[calc(var(--board-menu-icon-row-size)+var(--board-menu-padding))]",
                        expanded ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0",
                    )}
                    data-testid="board-hover-menu-workflows"
                    inert={!expanded}
                >
                    <header
                        className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-2)] px-[var(--space-2)] pt-[var(--space-2)] leading-none"
                        data-testid="board-hover-menu-header"
                    >
                        <h2 className="m-0 text-lg font-bold leading-none text-[var(--color-on-island)]">
                            {t("board.workflowPicker")}
                        </h2>
                        <button
                            aria-pressed={pinned}
                            aria-label={pinned ? t("board.unpinMenu") : t("board.pinMenu")}
                            className={cx(
                                "grid size-[24px] place-items-center rounded-full border border-transparent bg-transparent transition-colors hover:text-[var(--color-on-island)] focus-visible:border-[var(--color-primary)] focus-visible:outline-none",
                                pinned ? "text-[var(--color-primary)]" : "text-[var(--color-muted)]",
                            )}
                            onClick={() => {
                                setPinned((value) => !value);
                            }}
                            type="button"
                        >
                            {pinned ? (
                                <PinOff aria-hidden="true" data-testid="board-hover-menu-pin-off-icon" size={14} strokeWidth={1.8} />
                            ) : (
                                <Pin aria-hidden="true" data-testid="board-hover-menu-pin-icon" size={14} strokeWidth={1.8} />
                            )}
                        </button>
                    </header>
                    <ItemGroup className="gap-[var(--space-1)]">
                        {board.workflows.map((workflow) => (
                            <WorkflowOption
                                key={workflow.id}
                                onEdit={() => {
                                    onWorkflowEdit(workflow.id);
                                }}
                                onSelect={() => {
                                    onWorkflowSelect(workflow.id);
                                }}
                                workflow={workflow}
                            />
                        ))}
                    </ItemGroup>
                </div>
            </div>
            <div className="absolute bottom-[var(--board-menu-padding)] left-[var(--board-menu-padding)] flex h-10 shrink-0 items-center gap-[var(--board-menu-icon-gap)]" data-testid="board-hover-menu-actions">
                <IconMenuButton disabled={!canCreateTask} label={t("board.newTask")} onClick={onNewTask}>
                    <Plus aria-hidden="true" size={24} strokeWidth={1.6} />
                </IconMenuButton>
                <IconMenuButton disabled={!canCreateTask} label={t("workflowLibrary.linkWorkflow")} onClick={onWorkflowLink}>
                    <Link2 aria-hidden="true" size={22} strokeWidth={1.6} />
                </IconMenuButton>
            </div>
        </div>
    );
}

function IconMenuButton({
    children,
    disabled = false,
    label,
    onClick,
}: Readonly<{
    children: ReactNode;
    disabled?: boolean;
    label: string;
    onClick: () => void;
}>) {
    return (
        <button
            aria-label={label}
            className="grid h-10 w-10 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-45"
            disabled={disabled}
            onClick={onClick}
            type="button"
        >
            {children}
        </button>
    );
}

function WorkflowOption({
    workflow,
    onEdit,
    onSelect,
}: Readonly<{ onEdit: () => void; onSelect: () => void; workflow: WorkflowPickerItem }>) {
    const { t } = useTranslation();
    return (
        <div
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-1)] rounded-md"
            data-testid={`board-workflow-row-${workflow.id}`}
        >
            <Item
                className="gap-[var(--space-3)] px-[var(--space-2)] py-[var(--space-2)] text-[var(--color-on-island)]"
                onClick={onSelect}
            >
                <ItemContent>
                    <ItemTitle>{workflow.name}</ItemTitle>
                </ItemContent>
            </Item>
            <button
                aria-label={t("board.editWorkflow", { name: workflow.name })}
                className="grid size-[32px] place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-muted)] transition-colors hover:text-[var(--color-on-island)] focus-visible:border-[var(--color-primary)] focus-visible:outline-none"
                onClick={onEdit}
                title={t("board.editWorkflow", { name: workflow.name })}
                type="button"
            >
                <Pencil aria-hidden="true" size={15} strokeWidth={1.7} />
            </button>
        </div>
    );
}

function clearCollapseTimer(timer: { current: ReturnType<typeof setTimeout> | null }): void {
    if (timer.current !== null) {
        clearTimeout(timer.current);
        timer.current = null;
    }
}
