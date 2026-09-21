import type { BoardNodeCardsInput } from "./clientInputs";
import { create } from "@app/server-api-contract";
import { BoardReadService } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { taskLabelFilterPayload } from "./clientWorkflowLabels";
import { boardNodeCardsPageSize } from "./boardNodeCardsSorting";
import type { BoardNodeCardsPage, WorkflowBoard } from "./models";
import { boardNodeCardsPage, workflowBoard } from "./clientTaskProjection";
import { requireUnarySuccess } from "./protobufRpc";
import { throwWorkflowLabelFailure } from "./workflowLabelFailure";
import { taskSortField, taskSortDirection } from "./workflowProtoValues";
import type { DescriptorRpcTransport } from "./transport";
import type { BoardFilter } from "./workflowBoardFilters";

export async function getBoard(
  transport: DescriptorRpcTransport,
  projectID: string,
  workflowID: string | undefined,
  filter: BoardFilter,
): Promise<WorkflowBoard> {
  const method = BoardReadService.method.get;
  const result = await transport.callDescriptor(method, create(method.input, {
    projectId: projectID,
    workflowId: workflowID,
    labelFilter: taskLabelFilterPayload(filter.labelFilter),
    dependencyFilter: filter.dependencyFilter ?? undefined,
  }));
  throwWorkflowLabelFailure(method, result.outcome);
  return workflowBoard(requireUnarySuccess(method, result).board);
}

export async function listBoardNodeCards(
  transport: DescriptorRpcTransport,
  input: BoardNodeCardsInput,
): Promise<BoardNodeCardsPage> {
  const method = BoardReadService.method.listNodeCards;
  const result = await transport.callDescriptor(method, create(method.input, {
    projectId: input.projectID,
    workflowId: input.workflowID,
    nodeId: input.nodeID,
    labelFilter: taskLabelFilterPayload(input.filter.labelFilter),
    dependencyFilter: input.filter.dependencyFilter ?? undefined,
    pageSize: boardNodeCardsPageSize,
    sort: input.sort === undefined ? undefined : {
      field: taskSortField.encode(input.sort.field),
      direction: taskSortDirection.encode(input.sort.direction),
    },
    offset: input.offset ?? 0,
  }));
  throwWorkflowLabelFailure(method, result.outcome);
  return boardNodeCardsPage(requireUnarySuccess(method, result));
}
