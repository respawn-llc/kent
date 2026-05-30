package workflowstore

import (
	"strings"

	_ "embed"
)

var (
	//go:embed queries/interrupt_task_run_candidates.sql
	interruptTaskRunCandidatesQuery string
	//go:embed queries/interrupt_run_generation.sql
	interruptRunGenerationQuery string
	//go:embed queries/resolve_task_waiting_ask.sql
	resolveTaskWaitingAskQuery string
	//go:embed queries/resume_task_run_candidates.sql
	resumeTaskRunCandidatesQuery string
	//go:embed queries/resume_task_run.sql
	resumeTaskRunQuery string
	//go:embed queries/resolve_run_transition_context.sql
	resolveRunTransitionContextQuery string
	//go:embed queries/resolve_run_input_values.sql
	resolveRunInputValuesQuery string
	//go:embed queries/attach_run_session.sql
	attachRunSessionQuery string
	//go:embed queries/set_run_waiting_ask.sql
	setRunWaitingAskQuery string
	//go:embed queries/clear_run_waiting_ask.sql
	clearRunWaitingAskQuery string
	//go:embed queries/resolve_context_source_run.sql
	resolveContextSourceRunQuery string
	//go:embed queries/latest_node_output_value.sql
	latestNodeOutputValueQuery string
	//go:embed queries/join_expected_branches.sql
	joinExpectedBranchesQuery string
	//go:embed queries/join_arrivals.sql
	joinArrivalsQuery string
	//go:embed queries/unlink_project_workflow.sql
	unlinkProjectWorkflowQuery string
	//go:embed queries/project_workflow_unlink_state.sql
	projectWorkflowUnlinkStateQuery string
	//go:embed queries/start_task_complete_start_placement.sql
	startTaskCompleteStartPlacementQuery string
	//go:embed queries/complete_run_update_run.sql
	completeRunUpdateRunQuery string
	//go:embed queries/list_workflows.sql
	listWorkflowsQuery string
	//go:embed queries/ensure_workflow_transition_group_id.sql
	ensureWorkflowTransitionGroupIDQuery string
	//go:embed queries/latest_run_for_placement.sql
	latestRunForPlacementQuery string
	//go:embed queries/delete_workflow_tasks.sql
	deleteWorkflowTasksQuery string
	//go:embed queries/clear_deleted_workflow_default_project_links.sql
	clearDeletedWorkflowDefaultProjectLinksQuery string
	//go:embed queries/task_identity_for_transition.sql
	taskIdentityForTransitionQuery string
	//go:embed queries/task_identity_for_comment.sql
	taskIdentityForCommentQuery string
	//go:embed queries/list_workflow_project_links.sql
	listWorkflowProjectLinksQuery string
	//go:embed queries/update_workflow_edge.sql
	updateWorkflowEdgeQuery string
	//go:embed queries/upsert_workflow_edge.sql
	upsertWorkflowEdgeQuery string
	//go:embed queries/upsert_workflow_node_group.sql
	upsertWorkflowNodeGroupQuery string
	//go:embed queries/upsert_workflow_node.sql
	upsertWorkflowNodeQuery string
	//go:embed queries/upsert_workflow_transition_group.sql
	upsertWorkflowTransitionGroupQuery string
	//go:embed queries/update_workflow_node.sql
	updateWorkflowNodeQuery string
	//go:embed queries/update_workflow_transition_group.sql
	updateWorkflowTransitionGroupQuery string
	//go:embed queries/record_final_answer_protocol_violation.sql
	recordFinalAnswerProtocolViolationQuery string
	//go:embed queries/record_invalid_completion_protocol_violation.sql
	recordInvalidCompletionProtocolViolationQuery string
	//go:embed queries/manual_move_previous_transition.sql
	manualMovePreviousTransitionQuery string
)

func workflowStoreQuery(query string) string {
	return strings.TrimSuffix(query, "\n")
}

func workflowStoreQueryWithClause(query string, clause string) string {
	return strings.Replace(workflowStoreQuery(query), "{{clause}}", clause, 1)
}
