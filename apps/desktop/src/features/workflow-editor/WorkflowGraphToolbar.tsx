import { useReactFlow } from "@xyflow/react";
import { Info, Maximize2, Plus, RotateCcw, ZoomIn, ZoomOut } from "lucide-react";
import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import {
  IslandSurface,
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "../../ui";

export function WorkflowGraphToolbar({
  onAddNode,
  onWorkflowInspect,
}: Readonly<{
  onAddNode: ((kind: "agent" | "terminal") => void) | undefined;
  onWorkflowInspect: () => void;
}>) {
  const { t } = useTranslation();
  const instance = useReactFlow();
  return createPortal(
    <IslandSurface
      as="div"
      className="workflow-editor-tools app-region-no-drag fixed left-[var(--space-2)] top-[calc(var(--native-titlebar-height)+var(--space-2))] z-30 grid gap-[var(--space-1)] rounded-[var(--radius-l)] p-[var(--space-1)]"
      data-testid="workflow-editor-tools"
      level={1}
    >
      <CanvasTool label={t("workflowEditor.inspectWorkflow")} onClick={onWorkflowInspect}>
        <Info aria-hidden="true" size={18} strokeWidth={1.7} />
      </CanvasTool>
      <AddNodeTool disabled={onAddNode === undefined} onAddNode={onAddNode} />
      <CanvasTool label={t("workflowEditor.zoomIn")} onClick={() => void instance.zoomIn()}>
        <ZoomIn aria-hidden="true" size={18} strokeWidth={1.7} />
      </CanvasTool>
      <CanvasTool label={t("workflowEditor.zoomOut")} onClick={() => void instance.zoomOut()}>
        <ZoomOut aria-hidden="true" size={18} strokeWidth={1.7} />
      </CanvasTool>
      <CanvasTool
        label={t("workflowEditor.fitView")}
        onClick={() => void instance.fitView({ padding: 0.18 })}
      >
        <Maximize2 aria-hidden="true" size={18} strokeWidth={1.7} />
      </CanvasTool>
      <CanvasTool
        label={t("workflowEditor.resetZoom")}
        onClick={() => void instance.setViewport({ x: 0, y: 0, zoom: 1 })}
      >
        <RotateCcw aria-hidden="true" size={18} strokeWidth={1.7} />
      </CanvasTool>
    </IslandSurface>,
    document.body,
  );
}

function AddNodeTool({
  disabled,
  onAddNode,
}: Readonly<{ disabled: boolean; onAddNode: ((kind: "agent" | "terminal") => void) | undefined }>) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Popover onOpenChange={setOpen} open={open}>
      <PopoverTrigger asChild>
        <button
          aria-label={t("workflowEditor.addNode")}
          className="grid size-9 place-items-center rounded-[var(--radius-m)] border border-transparent bg-transparent text-[var(--color-on-island)] transition-colors hover:bg-[var(--color-island-1)] focus-visible:border-[var(--color-primary)] focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
          disabled={disabled}
          title={t("workflowEditor.addNode")}
          type="button"
        >
          <Plus aria-hidden="true" size={18} strokeWidth={1.7} />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[220px] gap-[var(--space-1)] p-[var(--space-2)]"
        side="right"
      >
        <CanvasMenuButton
          label={t("workflowEditor.addAgentNode")}
          onClick={() => {
            setOpen(false);
            onAddNode?.("agent");
          }}
        />
        <CanvasMenuButton
          label={t("workflowEditor.addTerminalNode")}
          onClick={() => {
            setOpen(false);
            onAddNode?.("terminal");
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

function CanvasMenuButton({ label, onClick }: Readonly<{ label: string; onClick: () => void }>) {
  return (
    <button
      className="rounded-[var(--radius-m)] border border-transparent bg-transparent px-[var(--space-3)] py-[var(--space-2)] text-left text-sm font-semibold text-[var(--color-on-island)] outline-none transition-colors hover:bg-[var(--color-island-2)] focus-visible:border-[var(--color-primary)]"
      onClick={onClick}
      type="button"
    >
      {label}
    </button>
  );
}

function CanvasTool({
  children,
  label,
  onClick,
}: Readonly<{ children: ReactNode; label: string; onClick: () => void }>) {
  return (
    <button
      aria-label={label}
      className="grid size-9 place-items-center rounded-[var(--radius-m)] border border-transparent bg-transparent text-[var(--color-on-island)] transition-colors hover:bg-[var(--color-island-1)] focus-visible:border-[var(--color-primary)] focus-visible:outline-none"
      onClick={onClick}
      title={label}
      type="button"
    >
      {children}
    </button>
  );
}
