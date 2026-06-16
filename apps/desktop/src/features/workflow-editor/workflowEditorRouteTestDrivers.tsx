// Shared React driver and fixture components for the split WorkflowEditorRoute tests.
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { queryKeys } from "../../app/queryKeys";
import { useSidebar } from "../../app/sidebarContext";
import { WorkflowGraphEdge } from "./WorkflowGraphEdge";
import { WorkflowInspectorSidebar } from "./WorkflowInspectorSidebar";
import { useWorkflowEditorDraftController } from "./workflowEditorDraftBridgeCore";
import { cachedValidation, cachedWorkflowDefinition } from "./workflowEditorRouteValidationFixtures";

export function CachedEdgeInspectorFixture({
  definition = cachedWorkflowDefinition,
  edgeID = "edge-2",
}: Readonly<{
  definition?: typeof cachedWorkflowDefinition | undefined;
  edgeID?: string | undefined;
}>) {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), definition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [definition, queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "edge", edgeID }} workflowID="workflow-1" />;
}

export function CachedNodeInspectorFixture() {
  const queryClient = useQueryClient();
  useEffect(() => {
    queryClient.setQueryData(queryKeys.workflowDefinition("workflow-1"), cachedWorkflowDefinition);
    queryClient.setQueryData(queryKeys.workflowValidation("workflow-1", "execution"), cachedValidation);
  }, [queryClient]);
  return <WorkflowInspectorSidebar selection={{ kind: "node", nodeID: "node-1" }} workflowID="workflow-1" />;
}

export function OpenStandardSidebar() {
  const { openSidebar } = useSidebar();
  return (
    <button
      onClick={() => {
        void openSidebar({
          content: <p>Default sidebar content</p>,
          kind: "custom",
          title: "Settings",
        });
      }}
      type="button"
    >
      Open standard sidebar
    </button>
  );
}

export function OpenEdgeInspectorButton({ edgeID = "edge-2" }: Readonly<{ edgeID?: string | undefined }>) {
  const { openSidebar } = useSidebar();
  return (
    <button
      onClick={() => {
        void openSidebar({
          kind: "workflowInspect",
          mode: "overlay",
          selection: { kind: "edge", edgeID },
          workflowID: "workflow-1",
        });
      }}
      type="button"
    >
      Open edge inspector
    </button>
  );
}

export function WorkflowConflictDriver() {
  const queryClient = useQueryClient();
  useOneShotWorkflowMetadataEdit();
  return (
    <button
      onClick={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.workflowDefinition("workflow-1") });
      }}
      type="button"
    >
      Simulate remote update
    </button>
  );
}

export function WorkflowMetadataEditDriver() {
  useOneShotWorkflowMetadataEdit();
  return null;
}

export function EdgeContextMenuDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  return (
    <div>
      <span data-testid="edge-delete-driver-edges">
        {controller.draft.edges.map((edge) => edge.id).join(",")}
      </span>
      <svg>
        <WorkflowGraphEdge
          data={{
            contextMode: "compact_and_continue_session",
            entityID: "edge-2",
            entityKind: "edge",
            hasError: false,
            label: "",
            routePoints: [
              { x: 0, y: 0 },
              { x: 100, y: 0 },
            ],
            transitionGroupID: "tg-2",
          }}
          id="edge-delete-driver"
          onDeleteSelection={(selection) => {
            if (selection.kind === "edge") {
              controller.dispatch({ edgeID: selection.edgeID, type: "deleteEdge" });
            }
          }}
          onInspect={() => undefined}
          onSelectContextMenu={() => undefined}
          source="join"
          sourceX={0}
          sourceY={0}
          target="done"
          targetX={100}
          targetY={0}
          type="workflow"
        />
      </svg>
    </div>
  );
}

export function WorkflowNodeGroupProbe({ nodeID, testID }: Readonly<{ nodeID: string; testID: string }>) {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const node = controller.draft.nodes.find((item) => item.id === nodeID);
  const groupID = node?.groupID;
  return <span data-testid={testID}>{groupID === undefined || groupID.length === 0 ? "none" : groupID}</span>;
}

export function WorkflowFanoutProbe() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const branchEdges = controller.draft.edges
    .filter((edge) => edge.targetNodeID === "impl-a" || edge.targetNodeID === "impl-b")
    .sort((left, right) => left.targetNodeID.localeCompare(right.targetNodeID));
  return (
    <div>
      <span data-testid="workflow-fanout-probe">
        {branchEdges.map((edge) => edge.transitionGroupID).join(",")}
      </span>
      <button
        onClick={() => {
          controller.dispatch({ edgeID: "edge-plan-implement", type: "deleteEdge" });
        }}
        type="button"
      >
        Delete stale branch edge
      </button>
      <button
        onClick={() => {
          controller.dispatch({
            input: {
              edgeID: "edge-plan-implement-repair",
              sourceNodeID: "plan",
              targetNodeID: "impl-b",
              transitionGroupID: "tg-unused-repair",
            },
            type: "connectNodes",
          });
        }}
        type="button"
      >
        Drag Plan to Implement
      </button>
    </div>
  );
}

export function StaleNativeGraphDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  return (
    <div>
      <span data-testid="stale-native-delete-driver-edges">
        {controller.draft.edges.map((edge) => edge.id).join(",")}
      </span>
      <button
        onClick={() => {
          controller.dispatch({
            input: {
              edgeID: "edge-stale",
              sourceNodeID: "node-1",
              targetNodeID: "done",
              transitionGroupID: "tg-stale",
            },
            type: "connectNodes",
          });
        }}
        type="button"
      >
        Add stale edge
      </button>
    </div>
  );
}

export function StalePromptNativeGraphDeleteDriver() {
  const controller = useWorkflowEditorDraftController("workflow-1");
  if (controller === null) {
    return null;
  }
  const promptTemplate = controller.draft.edges.find((edge) => edge.id === "edge-1")?.promptTemplate ?? "";
  return (
    <div>
      <span data-testid="stale-native-delete-driver-prompt">{promptTemplate}</span>
      <button
        onClick={() => {
          controller.dispatch({
            edgeID: "edge-1",
            promptTemplate: "Added stale prompt.",
            type: "editEdgePrompt",
          });
        }}
        type="button"
      >
        Add stale prompt
      </button>
    </div>
  );
}

function useOneShotWorkflowMetadataEdit() {
  const edited = useRef(false);
  const controller = useWorkflowEditorDraftController("workflow-1");
  useEffect(() => {
    if (controller === null || edited.current || controller.state.source.workflow.name.length === 0) {
      return;
    }
    edited.current = true;
    controller.dispatch({
      description: controller.draft.workflow.description,
      name: "Locally edited delivery",
      type: "editWorkflowMetadata",
    });
  }, [controller]);
}
