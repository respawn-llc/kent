import { EdgeLabelRenderer, type EdgeProps } from "@xyflow/react";
import type { MouseEvent } from "react";
import { useTranslation } from "react-i18next";

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger, IslandSurface } from "../../ui";
import { cx } from "../../ui/classes";
import { workflowEdgePath } from "./workflowEdgePath";
import { workflowEdgeColor } from "./workflowGraphColors";
import type { WorkflowGraphEdge as WorkflowGraphEdgeModel } from "./workflowGraphLayout";
import type { WorkflowGraphSelection } from "./workflowGraphSelection";
import { workflowGraphZOrder } from "./workflowGraphZOrder";

export function WorkflowGraphEdge(
  props: EdgeProps<WorkflowGraphEdgeModel> &
    Readonly<{
      onDeleteSelection?: ((selection: WorkflowGraphSelection) => void) | undefined;
      onInspect: (edgeID: string) => void;
      onSelectContextMenu?: ((edgeID: string) => void) | undefined;
    }>,
) {
  const { t } = useTranslation();
  const edgePath = workflowEdgePath(props);
  const label = props.data?.label ?? "";
  const color = edgeColor(props);
  const edgeID = props.data?.entityKind === "edge" ? props.data.entityID : "";
  const inspect = (event: MouseEvent) => {
    event.stopPropagation();
    if (edgeID.length > 0) {
      props.onInspect(edgeID);
    }
  };
  const selectFromContextMenu = () => {
    if (edgeID.length > 0) {
      props.onSelectContextMenu?.(edgeID);
    }
  };
  const deleteFromContextMenu = () => {
    if (edgeID.length > 0) {
      props.onDeleteSelection?.({ edgeID, kind: "edge" });
    }
  };
  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild onContextMenu={selectFromContextMenu}>
          <g>
            <path
              data-testid="workflow-edge-hit-path"
              data-edge-id={props.id}
              d={edgePath.path}
              onClick={inspect}
              style={{
                opacity: 0,
                pointerEvents: "stroke",
                stroke: "transparent",
                strokeLinecap: "round",
                strokeLinejoin: "round",
                strokeWidth: 18,
              }}
            />
            <path
              data-testid="workflow-edge-path"
              data-edge-id={props.id}
              d={edgePath.path}
              markerEnd={typeof props.markerEnd === "string" ? props.markerEnd : undefined}
              onClick={inspect}
              style={{
                fill: "none",
                pointerEvents: "stroke",
                stroke: color,
                strokeLinecap: "round",
                strokeLinejoin: "round",
                strokeWidth: props.selected ? 2.5 : 1.5,
              }}
            />
          </g>
        </ContextMenuTrigger>
        <ContextMenuContent level={3}>
          <ContextMenuItem onSelect={deleteFromContextMenu}>{t("workflowEditor.deleteEdge")}</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {label.length > 0 ? (
        <EdgeLabelRenderer>
          <ContextMenu>
            <ContextMenuTrigger asChild onContextMenu={selectFromContextMenu}>
              <IslandSurface
                as="div"
                className={cx(
                  "workflow-editor-edge-label absolute max-w-[180px] truncate rounded-full px-[var(--space-2)] py-[2px] text-xs font-semibold text-[var(--color-on-background)]",
                  props.data?.hasError === true ? "border-[var(--color-error)]" : "border-[var(--color-outline)]",
                )}
                data-testid={`workflow-edge-label-${props.id}`}
                level={1}
                onClick={inspect}
                style={{
                  transform: `translate(-50%, -50%) translate(${edgePath.labelPoint.x.toString()}px, ${edgePath.labelPoint.y.toString()}px)`,
                  zIndex: workflowGraphZOrder.edgeLabel,
                }}
                title={label}
              >
                {label}
              </IslandSurface>
            </ContextMenuTrigger>
            <ContextMenuContent level={3}>
              <ContextMenuItem onSelect={deleteFromContextMenu}>{t("workflowEditor.deleteEdge")}</ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

function edgeColor(props: EdgeProps<WorkflowGraphEdgeModel>): string {
  if (props.markerEnd !== undefined && typeof props.markerEnd !== "string" && props.markerEnd.color !== undefined) {
    return props.markerEnd.color;
  }
  return workflowEdgeColor(props.data?.contextMode ?? "", props.data?.hasError === true);
}
