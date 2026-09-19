import { create } from "@app/server-api-contract";
import {
  ProjectLinkDefaultMode,
  ProjectLinkService,
  WorkflowDefinitionService,
  WorkflowGraphService,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import type {
  WorkflowCreateAndLinkInput,
  WorkflowCreateInput,
  WorkflowDeleteInput,
  WorkflowGraphDeriveWiringInput,
  WorkflowGraphSaveInput,
  WorkflowGraphSavePreviewInput,
  WorkflowGraphValidateDraftInput,
  WorkflowListInput,
  WorkflowProjectLinkInput,
  WorkflowScriptPathValidateInput,
} from "./clientInputs";
import { workflowPageSize } from "./clientInputs";
import type { DescriptorRpcTransport } from "./transport";
import { requireUnarySuccess } from "./protobufRpc";
import {
  projectWorkflowLink,
  workflowDefinition,
  workflowDeleteImpact,
  workflowRecord,
  workflowSavePreview,
  workflowValidation,
  workflowValidationResults,
  workflowWiring,
} from "./clientWorkflowProjection";
import {
  workflowGraphDraftPayload,
  workflowGraphMetadataPayload,
  workflowGraphSaveConfirmationPayload,
} from "./clientWorkflowGraph";
import { workflowValidationMode } from "./workflowProtoValues";

export async function getWorkflow(transport: DescriptorRpcTransport, workflowID: string) {
  const method = WorkflowDefinitionService.method.get;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(method, create(method.input, { workflowId: workflowID })),
  );
  return workflowDefinition(success.definition);
}

export async function listWorkflows(transport: DescriptorRpcTransport, input: WorkflowListInput) {
  const method = WorkflowDefinitionService.method.list;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        offset: input.offset ?? 0,
        limit: input.limit ?? workflowPageSize,
        projectId: input.projectID,
        query: input.query ?? "",
      }),
    ),
  );
  return { workflows: success.workflows.map(workflowRecord), nextOffset: success.nextOffset ?? null };
}

export async function createWorkflow(transport: DescriptorRpcTransport, input: WorkflowCreateInput) {
  const method = WorkflowDefinitionService.method.create;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, { name: input.name, description: input.description }),
    ),
  );
  return workflowRecord(success.workflow);
}

export async function createAndLinkWorkflowToProject(
  transport: DescriptorRpcTransport,
  input: WorkflowCreateAndLinkInput,
) {
  const method = WorkflowDefinitionService.method.createAndLinkProject;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        name: input.name,
        description: input.description,
        projectId: input.projectID,
        defaultPolicy: ProjectLinkDefaultMode.WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE,
      }),
    ),
  );
  return { workflow: workflowRecord(success.workflow), link: projectWorkflowLink(success.link) };
}

export async function linkWorkflowToProject(
  transport: DescriptorRpcTransport,
  input: WorkflowProjectLinkInput,
) {
  const method = ProjectLinkService.method.link;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        projectId: input.projectID,
        workflowId: input.workflowID,
        defaultPolicy: ProjectLinkDefaultMode.WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE,
      }),
    ),
  );
  return projectWorkflowLink(success.link);
}

export async function validateWorkflow(
  transport: DescriptorRpcTransport,
  workflowID: string,
  mode: "draft" | "task_creation" | "execution",
) {
  const method = WorkflowDefinitionService.method.validate;
  return workflowValidation(
    requireUnarySuccess(
      method,
      await transport.callDescriptor(
        method,
        create(method.input, {
          workflowId: workflowID,
          mode: workflowValidationMode.encode(mode),
        }),
      ),
    ),
  );
}

export async function validateWorkflowScriptPath(
  transport: DescriptorRpcTransport,
  input: WorkflowScriptPathValidateInput,
) {
  const method = WorkflowDefinitionService.method.validateScriptPath;
  return workflowValidation(
    requireUnarySuccess(
      method,
      await transport.callDescriptor(
        method,
        create(method.input, {
          workflowId: input.workflowID,
          nodeId: input.nodeID,
          scriptPath: input.scriptPath,
        }),
      ),
    ),
  );
}

export async function validateWorkflowGraphDraft(
  transport: DescriptorRpcTransport,
  input: WorkflowGraphValidateDraftInput,
) {
  const method = WorkflowGraphService.method.validateDraft;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        workflowId: input.workflowID,
        metadata: workflowGraphMetadataPayload(input.metadata),
        graph: workflowGraphDraftPayload(input.graph),
        modes: input.modes.map((mode) => workflowValidationMode.encode(mode)),
      }),
    ),
  );
  return {
    ...workflowValidationResults(success.results),
    derivedWiring: workflowWiring(success.derivedWiring),
  };
}

export async function deriveWorkflowGraphWiring(
  transport: DescriptorRpcTransport,
  input: WorkflowGraphDeriveWiringInput,
) {
  const method = WorkflowGraphService.method.deriveWiring;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        workflowId: input.workflowID,
        graph: workflowGraphDraftPayload(input.graph),
      }),
    ),
  );
  return workflowWiring(success.derivedWiring);
}

export async function previewWorkflowGraphSave(
  transport: DescriptorRpcTransport,
  input: WorkflowGraphSavePreviewInput,
) {
  const method = WorkflowGraphService.method.savePreview;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        workflowId: input.workflowID,
        expectedVersion: BigInt(input.expectedVersion),
        metadata: workflowGraphMetadataPayload(input.metadata),
        graph: workflowGraphDraftPayload(input.graph),
      }),
    ),
  );
  return workflowSavePreview(success);
}

export async function saveWorkflowGraph(transport: DescriptorRpcTransport, input: WorkflowGraphSaveInput) {
  const method = WorkflowGraphService.method.save;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        workflowId: input.workflowID,
        expectedVersion: BigInt(input.expectedVersion),
        metadata: workflowGraphMetadataPayload(input.metadata),
        graph: workflowGraphDraftPayload(input.graph),
        confirmation: workflowGraphSaveConfirmationPayload(input.confirmation),
      }),
    ),
  );
  return {
    ...workflowSavePreview(success),
    saved: success.saved,
    definition: success.definition === undefined ? null : workflowDefinition(success.definition),
  };
}

export async function previewWorkflowDelete(transport: DescriptorRpcTransport, workflowID: string) {
  const method = WorkflowDefinitionService.method.deletePreview;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(method, create(method.input, { workflowId: workflowID })),
  );
  return workflowDeleteImpact(success.impact);
}

export async function deleteWorkflow(transport: DescriptorRpcTransport, input: WorkflowDeleteInput) {
  const method = WorkflowDefinitionService.method.delete;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(
      method,
      create(method.input, {
        workflowId: input.workflowID,
        confirmed: input.confirmed,
        expectedVersion: BigInt(input.expectedVersion),
        expectedProjectCount: BigInt(input.expectedProjectCount),
        expectedLinkCount: BigInt(input.expectedLinkCount),
        expectedTaskCount: BigInt(input.expectedTaskCount),
        cleanupArtifacts: input.cleanupArtifacts ?? false,
      }),
    ),
  );
  return {
    deleted: success.deleted,
    impact: workflowDeleteImpact(success.impact),
    blockers: success.blockers.map((blocker) => ({
      code: blocker.code,
      message: blocker.message,
      count: Number(blocker.count),
    })),
  };
}

export async function listProjectWorkflowLinks(transport: DescriptorRpcTransport, projectID: string) {
  const method = ProjectLinkService.method.list;
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptor(method, create(method.input, { projectId: projectID })),
  );
  return success.links.map(projectWorkflowLink);
}
