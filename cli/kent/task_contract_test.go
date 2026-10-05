package main

import (
	"bytes"
	"context"
	"encoding/json"

	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
	"core/shared/sessionenv"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type taskPaginationStub struct {
	apicontract.WorkflowService
	taskListRequest  *taskpb.ListRequest
	taskListResponse *taskpb.ListSuccess
}

type taskSearchServiceStub struct {
	apicontract.ProjectViewService
	apicontract.WorkflowService
	request  *taskpb.SearchRequest
	response *taskpb.SearchSuccess
	err      error
}

func (s *taskSearchServiceStub) SearchWorkflowTasks(
	_ context.Context,
	request *taskpb.SearchRequest,
) (*taskpb.SearchSuccess, error) {
	s.request = request
	return s.response, s.err
}

func (s *taskPaginationStub) ListWorkflowTasks(
	_ context.Context,
	request *taskpb.ListRequest,
) (*taskpb.ListSuccess, error) {
	s.taskListRequest = request
	return s.taskListResponse, nil
}

func TestTaskListPureFilterSortAndPaginationContracts(t *testing.T) {
	values, err := parseTaskListFilterValues([]string{"active,done", "active"}, "status")
	if err != nil || !slices.Equal(values, []string{"active", "done"}) {
		t.Fatalf("values=%v err=%v", values, err)
	}
	if _, err := parseTaskListFilterValues([]string{"active, "}, "status"); err == nil {
		t.Fatal("blank filter value accepted")
	}

	statuses, err := parseTaskListStatusKinds([]string{"active,done"})
	if err != nil || !slices.Equal(statuses, []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE, taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE}) {
		t.Fatalf("statuses=%v err=%v", statuses, err)
	}
	if _, err := parseTaskListStatusKinds([]string{"future"}); err == nil {
		t.Fatal("unknown status accepted")
	}
	attention, err := parseTaskListAttentionKinds([]string{"question,interrupted"})
	if err != nil || !slices.Equal(attention, []taskpb.TaskAttentionKind{taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_QUESTION, taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_INTERRUPTED}) {
		t.Fatalf("attention=%v err=%v", attention, err)
	}

	sortSelectors, err := parseTaskListSortSelectors([]string{
		"labels:desc,short_id:asc,created:asc",
		"updated:desc,status:asc,column:desc,title:asc",
	})
	if err != nil || len(sortSelectors) != 7 {
		t.Fatalf("sort=%v err=%v", sortSelectors, err)
	}
	if sortSelectors[0].Field != taskpb.ListSortField_LIST_SORT_FIELD_LABELS ||
		sortSelectors[1].Field != taskpb.ListSortField_LIST_SORT_FIELD_SHORT_ID ||
		sortSelectors[6].Field != taskpb.ListSortField_LIST_SORT_FIELD_TITLE {
		t.Fatalf("sort=%+v", sortSelectors)
	}
	for _, invalid := range []string{
		"title",
		"title:sideways",
		"title:asc,title:desc",
		"label:asc",
	} {
		if _, err := parseTaskListSortSelectors([]string{invalid}); err == nil {
			t.Fatalf("invalid sort %q accepted", invalid)
		}
	}

	if _, err := parseTaskListLabelMatch("all", true, 0, false); err == nil {
		t.Fatal("label match without selectors accepted")
	}
	if _, err := parseTaskListLabelMatch("any", false, 1, true); err == nil {
		t.Fatal("unlabeled with selector accepted")
	}
	if mode, err := parseTaskListLabelMatch("all", true, 2, false); err != nil ||
		mode != taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ALL {
		t.Fatalf("label mode=%q err=%v", mode, err)
	}

	if err := validateWorkflowPagination(0, taskListDefaultLimit); err != nil {
		t.Fatalf("valid pagination: %v", err)
	}
	for _, window := range [][2]int{{-1, 1}, {0, 0}, {0, serverapi.WorkflowPaginationMaxLimit + 1}} {
		if err := validateWorkflowPagination(window[0], window[1]); err == nil {
			t.Fatalf("invalid pagination %v accepted", window)
		}
	}
}

func TestTaskListDependencyFilterAndRetryArguments(t *testing.T) {
	for _, test := range []struct {
		name              string
		unblocked         bool
		unblockedProvided bool
		blocked           bool
		blockedProvided   bool
		want              *bool
		wantError         bool
	}{
		{name: "none"},
		{name: "unblocked", unblocked: true, unblockedProvided: true, want: boolTaskPointer(true)},
		{name: "blocked", blocked: true, blockedProvided: true, want: boolTaskPointer(false)},
		{name: "mutually exclusive", unblocked: true, unblockedProvided: true, blocked: true, blockedProvided: true, wantError: true},
		{name: "explicit false", unblockedProvided: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseTaskListDependencyFilter(
				test.unblocked,
				test.unblockedProvided,
				test.blocked,
				test.blockedProvided,
			)
			if (err != nil) != test.wantError || !equalBoolTaskPointers(got, test.want) {
				t.Fatalf("filter=%v err=%v", got, err)
			}
		})
	}

	mode := "all"
	blocked := false
	args := taskListRetryCommandArgs(taskListCommandContext{
		ProjectRef:             "project-ref",
		StatusKinds:            []string{"active"},
		AttentionKinds:         []string{"question"},
		ColumnKeys:             []string{"build"},
		Sort:                   []string{"updated:desc"},
		LabelSelectors:         []string{"Alpha"},
		ExcludedLabelSelectors: []string{"Beta"},
		LabelMatch:             &mode,
		DependencyFilter:       &blocked,
		Limit:                  100,
		JSON:                   true,
	}, nil)
	want := []string{
		config.Command, "task", "list", "--project", "project-ref",
		"--status", "active",
		"--attention", "question",
		"--column", "build",
		"--sort", "updated:desc",
		"--label", "Alpha",
		"--not-label", "Beta",
		"--label-match", "all",
		"--blocked",
		"--limit", "100",
		"--json",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("retry args=%v want=%v", args, want)
	}

	placeholder := taskListRetryCommandArgsForSelector(
		taskListCommandContext{ProjectRef: "project-ref", Limit: 100},
		taskWorkflowRetryWorkflowPlaceholder{},
	)
	if !slices.Equal(placeholder, []string{
		config.Command, "task", "list", "--project", "project-ref",
		"--workflow", "<uuid>", "--limit", "100",
	}) {
		t.Fatalf("placeholder retry=%v", placeholder)
	}
}

func TestTaskListAndCommentPaginationSuccess(t *testing.T) {
	offset, limit, nextOffset := int32(5), int32(2), int32(7)
	stub := &taskPaginationStub{
		taskListResponse: &taskpb.ListSuccess{
			Scope: &taskpb.ListScope{
				ProjectId: "project-1",
			},
			MatchingWorkflowCardinality: taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_NONE,
			Tasks:                       []*taskpb.ListItem{},
			NextOffset:                  &nextOffset,
		},
	}
	response, err := workflowTaskList(t.Context(), stub, &taskpb.ListRequest{
		ProjectId: func() *string {
			projectID := "project-1"
			return &projectID
		}(),
		Offset: &offset,
		Limit:  &limit,
	})
	if err != nil ||
		stub.taskListRequest.Offset == nil ||
		*stub.taskListRequest.Offset != offset ||
		stub.taskListRequest.Limit == nil ||
		*stub.taskListRequest.Limit != limit {
		t.Fatalf("request=%+v response=%+v err=%v", stub.taskListRequest, response, err)
	}

	var stdout, stderr bytes.Buffer
	if code := writeTaskListResponse(&stdout, &stderr, response, true); code != 0 || stderr.Len() != 0 {
		t.Fatalf("JSON exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output struct {
		NextOffset *int              `json:"next_offset"`
		Tasks      []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil ||
		output.NextOffset == nil ||
		*output.NextOffset != int(nextOffset) ||
		len(output.Tasks) != 0 {
		t.Fatalf("output=%+v err=%v", output, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := writeTaskListResponse(&stdout, &stderr, response, false); code != 0 ||
		stdout.Len() != 0 ||
		stderr.Len() == 0 {
		t.Fatalf("human exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := writeTaskCommentListResponse(&stdout, &stderr, &taskpb.CommentListSuccess{
		Items: []*taskpb.Comment{}, NextOffset: &nextOffset,
	}); code != 0 ||
		stdout.Len() != 0 ||
		stderr.Len() == 0 {
		t.Fatalf("comment exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTaskListCommandRejectsPaginationAndRemovedFlagsBeforeRemote(t *testing.T) {
	for _, test := range []struct {
		args []string
		code int
	}{
		{args: []string{"--offset", "-1"}, code: 2},
		{args: []string{"--limit", "0"}, code: 2},
		{args: []string{"--limit", "101"}, code: 2},
		{args: []string{"--page-token", "legacy"}, code: 2},
		{args: []string{"--page-size", "1"}, code: 2},
		{args: []string{"extra"}, code: 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := taskListSubcommand(test.args, &stdout, &stderr); code != test.code ||
			stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%q exit=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestTaskCommentListRejectsInvalidPaginationBeforeRemote(t *testing.T) {
	for _, args := range [][]string{
		{"DIS-1", "--offset", "-1"},
		{"DIS-1", "--limit", "0"},
		{"DIS-1", "--limit", "101"},
	} {
		var stdout, stderr bytes.Buffer
		if code := taskCommentListSubcommand(args, &stdout, &stderr); code != 2 ||
			stdout.Len() != 0 ||
			stderr.Len() == 0 {
			t.Fatalf("args=%q exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestTaskCommentAddCannotSpoofUserAuthorFromAgentSession(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "018fdd67-89ab-4cde-8123-456789abcdef")

	author := taskCommentAuthorForAdd(t.Context(), nil, "task-1", "user", true)

	if author.Kind != "agent" {
		t.Fatalf("author kind = %q, want agent", author.Kind)
	}
}

func TestTaskSearchExecutionProjectsScopeAndTypedOutcomes(t *testing.T) {
	nextOffset := int32(7)
	request := &taskpb.SearchRequest{
		Mode:            taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:           "title:needle",
		Context:         7,
		IncludeComments: true,
		StatusKinds:     []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE, taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE},
		PageSize:        2,
		Offset:          proto.Int32(4),
	}
	response := &taskpb.SearchSuccess{
		Mode:       taskpb.SearchMode_SEARCH_MODE_FTS5,
		Groups:     []*taskpb.SearchGroup{},
		NextOffset: &nextOffset,
	}
	stub := &taskSearchServiceStub{response: response}
	var stdout, stderr bytes.Buffer
	if code := runTaskSearch(
		t.Context(),
		config.Connection{},
		stub,
		stub,
		[]string{"project-b", "project-a", "project-b"},
		request,
		true,
		&stdout,
		&stderr,
	); code != 0 || stdout.Len() == 0 || stderr.Len() == 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !slices.Equal(stub.request.ProjectIds, []string{"project-a", "project-b"}) ||
		stub.request.Mode != request.Mode ||
		stub.request.Query != request.Query ||
		stub.request.Context != request.Context ||
		stub.request.IncludeComments != request.IncludeComments ||
		!slices.Equal(stub.request.StatusKinds, request.StatusKinds) ||
		stub.request.PageSize != request.PageSize ||
		stub.request.Offset == nil ||
		*stub.request.Offset != *request.Offset {
		t.Fatalf("request=%+v", stub.request)
	}
	var output taskSearchJSON
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil ||
		output.Mode != "fts5" ||
		output.NextOffset == nil ||
		*output.NextOffset != nextOffset ||
		len(output.Groups) != 0 {
		t.Fatalf("output=%+v err=%v", output, err)
	}

	stub = &taskSearchServiceStub{
		err: &serverapi.TaskSearchError{
			Reason: serverapi.TaskSearchErrorReasonNormalizedTooShort,
		},
	}
	stdout.Reset()
	stderr.Reset()
	if code := runTaskSearch(
		t.Context(),
		config.Connection{},
		stub,
		stub,
		nil,
		request,
		false,
		&stdout,
		&stderr,
	); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("failure exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTaskDependencyDirectionRenderingAndTypedJSON(t *testing.T) {
	for raw, want := range map[string]*taskpb.DependencyDirection{
		"":           nil,
		"blocks":     taskDependencyDirectionPointer(taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS),
		"blocked-by": taskDependencyDirectionPointer(taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY),
	} {
		got, err := parseTaskDependencyDirection(raw)
		if err != nil || !equalTaskDependencyDirections(got, want) {
			t.Fatalf("raw=%q direction=%v err=%v", raw, got, err)
		}
	}
	if _, err := parseTaskDependencyDirection("upstream"); err == nil {
		t.Fatal("invalid dependency direction accepted")
	}

	directions := []*taskpb.DependencyListDirection{
		{
			Direction:  taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY,
			TotalCount: 1,
			Items: []*taskpb.DependencyItem{{
				TaskId: "task-1", ShortId: "KENT-1", Title: "Foundation",
				Status: taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE),
			}},
		},
		{
			Direction:  taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS,
			TotalCount: 1,
			Items: []*taskpb.DependencyItem{{
				TaskId: "task-2", ShortId: "KENT-2", Title: "Follow-up",
				Status: taskContractStatus(taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG),
			}},
		},
	}
	ordered := taskDependencyDirectionsForRender(directions)
	if len(ordered) != 2 ||
		ordered[0].Direction != taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS ||
		ordered[0].Items[0].TaskId != "task-2" ||
		ordered[1].Direction != taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY ||
		ordered[1].Items[0].TaskId != "task-1" {
		t.Fatalf("ordered directions=%+v", ordered)
	}
	var stdout bytes.Buffer
	if err := writeTaskDependencyDirections(&stdout, directions); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatalf("render=%q", stdout.String())
	}

	response := &taskpb.DependencyMutationSuccess{
		Outcome:        taskpb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ALREADY_ABSENT,
		BlockerTaskId:  "task-1",
		BlockerShortId: "KENT-1",
		BlockedTaskId:  "task-2",
		BlockedShortId: "KENT-2",
	}
	stdout.Reset()
	var stderr bytes.Buffer
	if code := writeCommandJSON(&stdout, &stderr, response); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"outcome", "blocker_task_id", "blocker_short_id", "blocked_task_id", "blocked_short_id"} {
		if fields[key] == nil {
			t.Fatalf("JSON omitted %q: %s", key, stdout.String())
		}
	}
}

func TestTaskMoveStructuredValuesSelectionAndDependencyGuidance(t *testing.T) {
	values, err := readManualMoveValues(
		`{"build":{"artifact":"release.zip"},"test":{"result":"passed"}}`,
		"",
		true,
		false,
	)
	if err != nil || values["build"]["artifact"] != "release.zip" || values["test"]["result"] != "passed" {
		t.Fatalf("values=%v err=%v", values, err)
	}
	if _, err := readManualMoveValues("{}", "values.json", true, true); err == nil {
		t.Fatal("combined values sources accepted")
	}
	if _, err := readManualMoveValues("null", "", true, false); err == nil {
		t.Fatal("null values object accepted")
	}
	path := filepath.Join(t.TempDir(), "values.json")
	if err := os.WriteFile(path, []byte(`{"node":{"output":"value"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fileValues, err := readManualMoveValues("", path, false, true)
	if err != nil || fileValues["node"]["output"] != "value" {
		t.Fatalf("file values=%v err=%v", fileValues, err)
	}

	preview := &taskpb.MovePreviewSuccess{
		Outcome: &taskpb.MovePreviewSuccess_Transition{Transition: &taskpb.MovePreviewTransition{Choices: []*taskpb.MoveTransitionChoice{
			{TransitionKey: "approve"},
			{TransitionKey: "revise"},
		}}},
	}
	if _, err := selectTaskMoveTransition(preview, "", false); err == nil {
		t.Fatal("ambiguous transition auto-selected")
	}
	selected, err := selectTaskMoveTransition(preview, " revise ", true)
	if err != nil || selected == nil || *selected != "revise" {
		t.Fatalf("selected=%v err=%v", selected, err)
	}
	if _, err := selectTaskMoveTransition(preview, "missing", true); err == nil {
		t.Fatal("unknown transition accepted")
	}
	preview.GetTransition().Choices = preview.GetTransition().Choices[:1]
	selected, err = selectTaskMoveTransition(preview, "", false)
	if err != nil || selected == nil || *selected != "approve" {
		t.Fatalf("auto selection=%v err=%v", selected, err)
	}

	count := 2
	var stderr bytes.Buffer
	writeTaskDependencyConfirmationRequired(&stderr, "KENT-2", &count)
	if stderr.Len() == 0 {
		t.Fatalf("guidance=%q", stderr.String())
	}
	const recoveryCommand = "kent task move 11111111-1111-4111-8111-111111111111 22222222-2222-4222-8222-222222222222"
	stderr.Reset()
	writeTaskDependencyConfirmationRequiredForCommand(&stderr, "KENT-2", &count, recoveryCommand)
	if !strings.Contains(stderr.String(), recoveryCommand+" --ignore-dependencies") {
		t.Fatalf("forced-completion dependency guidance=%q", stderr.String())
	}
	stderr.Reset()
	writeWorkflowExecutionTargetSelectionRequiredForCommand(
		&stderr,
		serverapi.NewWorkflowPolicyTargetSelectionRequirement(),
		recoveryCommand,
	)
	if !strings.Contains(stderr.String(), recoveryCommand+" --execution-target head") {
		t.Fatalf("forced-completion selection guidance=%q", stderr.String())
	}

	response := &taskpb.MoveSuccess{Outcome: &taskpb.MoveSuccess_DependencyConfirmationRequired{
		DependencyConfirmationRequired: &taskpb.DependencyConfirmationRequired{UnsatisfiedDependencyCount: int32(count)},
	}}
	output, err := taskMoveOutput(response)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	stderr.Reset()
	if code := writeCommandJSON(&stdout, &stderr, output); code != 0 || stderr.Len() != 0 {
		t.Fatalf("dependency JSON exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields["outcome"] == nil || fields["unsatisfied_dependency_count"] == nil {
		t.Fatalf("dependency JSON=%s", stdout.String())
	}
}

func TestTaskSetupGuidanceContracts(t *testing.T) {
	script := "/repo/setup.sh"
	failed := &worktreepb.SetupEvent{
		Phase: &worktreepb.SetupEvent_Failed{
			Failed: &worktreepb.SetupFailed{
				RecoveryDisposition: worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING,
				RetryReadiness:      worktreepb.SetupRetryReadiness_WORKTREE_SETUP_RETRY_READY,
				Cause: &worktreepb.SetupFailureCause{
					Cause: &worktreepb.SetupFailureCause_ProcessExit{
						ProcessExit: &worktreepb.SetupProcessExit{ExitCode: 1},
					},
				},
				Diagnostic:               "failed twice",
				ScriptPath:               &script,
				ExecutionTarget:          &worktreepb.SetupExecutionTargetSelection{Mode: worktreepb.SetupExecutionTargetMode_WORKTREE_SETUP_EXECUTION_TARGET_MODE_HEAD},
				RetainedWorktree:         taskContractSetupWorktree("/tmp/retained").GetRegistered(),
				RetainedPreviousWorktree: &worktreepb.RetainedPreviousWorktree{Worktree: taskContractSetupWorktree("/tmp/previous").GetRegistered()},
			},
		},
	}
	start, err := projectTaskSetupGuidance(taskSetupObservedActionStart, "task-1", nil, failed, nil)
	if err != nil ||
		start.Outcome != taskSetupOutcomeObservedSetupFailure ||
		start.RetainedRoot == nil ||
		*start.RetainedRoot != "/tmp/retained" ||
		start.RetainedPreviousRoot == nil ||
		len(start.Actions) != 5 ||
		start.Actions[0].Kind != taskSetupActionRetry ||
		start.Actions[0].Args[2] != "start" ||
		start.Actions[0].Args[len(start.Actions[0].Args)-1] != "head" {
		t.Fatalf("start setup guidance=%+v err=%v", start, err)
	}
	resume, err := projectTaskSetupGuidance(taskSetupObservedActionResume, "task-1", nil, failed, nil)
	if err != nil || resume.Outcome != taskSetupOutcomeObservedSetupFailure ||
		resume.Actions[0].Args[2] != "resume" {
		t.Fatalf("resume setup guidance=%+v err=%v", resume, err)
	}
	completed, err := projectTaskSetupGuidance(taskSetupObservedActionStart, "task-1", nil, &worktreepb.SetupEvent{
		Phase: &worktreepb.SetupEvent_NotRequired{
			NotRequired: &worktreepb.SetupNotRequired{
				Reason:                   worktreepb.SetupNotRequiredReason_WORKTREE_SETUP_NOT_REQUIRED_REASON_NO_CONFIGURED_SCRIPT,
				RetainedPreviousWorktree: &worktreepb.RetainedPreviousWorktree{Worktree: taskContractSetupWorktree("/tmp/orphan").GetRegistered()},
			},
		},
	}, nil)
	if err != nil ||
		completed.Outcome != taskSetupOutcomeCompleted ||
		completed.RetainedPreviousRoot == nil ||
		len(completed.Actions) != 1 ||
		completed.Actions[0].Kind != taskSetupActionListWorktrees {
		t.Fatalf("completed setup guidance=%+v err=%v", completed, err)
	}
	observation, err := projectTaskSetupGuidance(
		taskSetupObservedActionStart,
		"task-1",
		nil,
		nil,
		context.DeadlineExceeded,
	)
	if err != nil ||
		observation.Outcome != taskSetupOutcomeObservationFailure ||
		len(observation.Actions) != 2 {
		t.Fatalf("observation guidance=%+v err=%v", observation, err)
	}
}

func TestTaskMoveSetupRecoveryPreservesStructuredInput(t *testing.T) {
	project, commentary, transition := "project-1", "note", "next"
	base, err := taskMoveRecoveryArgs(
		"task-1",
		"done",
		&project,
		&commentary,
		&transition,
		map[string]map[string]string{"plan": {"summary": "done"}},
		true,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD}
	setupErr := &worktreecontract.SetupRetainedError{
		Details: &worktreepb.SetupRetainedDetails{
			RecoveryDisposition:      worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_RETRY_EXISTING,
			Worktree:                 taskContractSetupWorktree("/tmp/retained").GetRegistered(),
			Diagnostic:               "failed twice",
			ScriptPath:               "/repo/setup.sh",
			RetainedPreviousWorktree: &worktreepb.RetainedPreviousWorktree{Worktree: taskContractSetupWorktree("/tmp/previous").GetRegistered()},
		},
	}
	decoded := &worktreecontract.SetupRetainedError{Details: proto.Clone(setupErr.Details).(*worktreepb.SetupRetainedDetails)}
	guidance, err := projectRetainedSetupGuidance(base, target, decoded)
	if err != nil ||
		guidance.Outcome != taskSetupOutcomeMoveSetupFailure ||
		guidance.RetainedRoot == nil ||
		*guidance.RetainedRoot != "/tmp/retained" ||
		guidance.RetainedPreviousRoot == nil ||
		*guidance.RetainedPreviousRoot != "/tmp/previous" ||
		len(guidance.Actions) != 5 ||
		!slices.Contains(guidance.Actions[0].Args, `{"plan":{"summary":"done"}}`) {
		t.Fatalf("move setup guidance=%+v err=%v", guidance, err)
	}
	decoded.Details.Diagnostic = " "
	if _, err := projectRetainedSetupGuidance(base, target, decoded); err == nil {
		t.Fatal("missing retained setup diagnostic accepted")
	}
	decoded.Details.Diagnostic = setupErr.Details.Diagnostic
	decoded.Details.Worktree.Kent.CanonicalRoot = "/tmp/different"
	if _, err := projectRetainedSetupGuidance(base, target, decoded); err == nil {
		t.Fatal("mismatched retained setup root accepted")
	}
	decoded.Details.Worktree.Kent.CanonicalRoot = decoded.Details.Worktree.Git.CanonicalRoot
	decoded.Details.RetainedPreviousWorktree.Worktree.Kent.CanonicalRoot = "/tmp/different"
	if _, err := projectRetainedSetupGuidance(base, target, decoded); err == nil {
		t.Fatal("mismatched previous retained setup root accepted")
	}
}

func TestTaskMoveReplacementSetupFailureRequiresFreshBranch(t *testing.T) {
	base := []string{config.Command, "task", "move", "task-1", "node-1"}
	setupErr := &worktreecontract.SetupRetainedError{
		Details: &worktreepb.SetupRetainedDetails{
			RecoveryDisposition: worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT,
			Worktree:            taskContractSetupWorktree("/tmp/retained").GetRegistered(),
			Diagnostic:          "setup process failed", ScriptPath: "/repo/setup.sh",
		},
	}
	guidance, err := projectRetainedSetupGuidance(base, nil, setupErr)
	if err != nil {
		t.Fatal(err)
	}
	if len(guidance.Actions) != 4 {
		t.Fatalf("replacement choices = %+v", guidance.Actions)
	}
	for _, action := range guidance.Actions {
		if action.Kind == taskSetupActionRetry {
			t.Fatal("replacement failure offered retry in retained root")
		}
		if slices.Contains(action.Args, "--branch-name") != (action.Kind != taskSetupActionChooseNone) {
			t.Fatalf("replacement branch flags = %+v", action)
		}
	}
	var stdout, stderr bytes.Buffer
	output, err := taskSetupRetainedOutput(setupErr.Details)
	if err != nil {
		t.Fatal(err)
	}
	if code := writeCommandJSON(&stdout, &stderr, output); code != 0 {
		t.Fatalf("structured failure serialization = %d: %s", code, stderr.String())
	}
	var decoded taskSetupRetainedJSON
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil ||
		decoded.Diagnostic != setupErr.Details.Diagnostic || decoded.Worktree.Registered.Git.CanonicalRoot != "/tmp/retained" {
		t.Fatalf("structured replacement diagnostics lost: %v", err)
	}
}

func TestWorktreeHeaderAndBranchCleanupPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		delete      bool
		forceDelete bool
		want        worktreepb.BranchCleanupMode
		wantError   bool
	}{
		{name: "retain", want: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN},
		{name: "safe delete", delete: true, want: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE},
		{name: "force delete", delete: true, forceDelete: true, want: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_FORCE},
		{name: "force requires delete", forceDelete: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := worktreeBranchCleanupPolicy(test.delete, test.forceDelete)
			if (err != nil) != test.wantError || got != test.want {
				t.Fatalf("policy=%q err=%v", got, err)
			}
		})
	}
}

func TestTaskDispatchAndDependencyAliasesRemainAvailable(t *testing.T) {
	var canonical bytes.Buffer
	if code := taskSubcommand([]string{"dep", "--help"}, &canonical, &canonical); code != 0 {
		t.Fatalf("canonical help exit=%d output=%q", code, canonical.String())
	}
	for _, alias := range []string{"deps", "dependency", "dependencies"} {
		var output bytes.Buffer
		if code := taskSubcommand([]string{alias, "--help"}, &output, &output); code != 0 ||
			output.Len() == 0 {
			t.Fatalf("alias=%q exit=%d output=%q", alias, code, output.String())
		}
	}
	var taskHelp bytes.Buffer
	if code := taskSubcommand([]string{"--help"}, &taskHelp, &taskHelp); code != 0 ||
		taskHelp.Len() == 0 {
		t.Fatalf("task help exit=%d output=%q", code, taskHelp.String())
	}
}

func boolTaskPointer(value bool) *bool { return &value }

func equalBoolTaskPointers(left *bool, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func taskDependencyDirectionPointer(value taskpb.DependencyDirection) *taskpb.DependencyDirection {
	return &value
}

func equalTaskDependencyDirections(
	left *taskpb.DependencyDirection,
	right *taskpb.DependencyDirection,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func taskContractStatus(kind taskpb.TaskStatusKind) *taskpb.TaskStatus {
	native, err := protoapi.TaskNativeState(kind)
	if err != nil {
		panic("invalid task status")
	}
	return &taskpb.TaskStatus{Kind: kind, NativeState: native}
}

func taskContractSetupWorktree(root string) *worktreepb.TopologyEntry {
	return &worktreepb.TopologyEntry{
		Topology: &worktreepb.TopologyEntry_Registered{
			Registered: &worktreepb.RegisteredFacts{
				Git:  &worktreepb.GitFacts{CanonicalRoot: root, HeadObject: "0123456789abcdef"},
				Kent: &worktreepb.KentFacts{WorktreeId: "worktree-1", CanonicalRoot: root, DisplayName: "KENT-453", Managed: true},
			},
		},
	}
}

func unsetEnvironmentForTaskContractTest(t *testing.T, name string) {
	t.Helper()
	value, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, value)
			return
		}
		_ = os.Unsetenv(name)
	})
}
