package workflowview

import (
	"core/internal/testharness/workflowfixture"
	"testing"

	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
)

type currentNodeLabelFilterFixture struct {
	currentNodeViewFixture
	alpha   string
	beta    string
	gamma   string
	taskIDs map[string]string
}

func newCurrentNodeLabelFilterFixture(t *testing.T) currentNodeLabelFilterFixture {
	t.Helper()
	current := newCurrentNodeViewFixture(t, false)
	alpha, err := current.store.CreateProjectLabel(current.ctx, current.binding.ProjectID, "alpha")
	if err != nil {
		t.Fatalf("CreateProjectLabel alpha: %v", err)
	}
	beta, err := current.store.CreateProjectLabel(current.ctx, current.binding.ProjectID, "beta")
	if err != nil {
		t.Fatalf("CreateProjectLabel beta: %v", err)
	}
	gamma, err := current.store.CreateProjectLabel(current.ctx, current.binding.ProjectID, "gamma")
	if err != nil {
		t.Fatalf("CreateProjectLabel gamma: %v", err)
	}
	startTask := func(title string, labelIDs ...string) string {
		t.Helper()
		workflowID := current.workflowID
		task, createErr := current.store.CreateTask(current.ctx, workflowstore.CreateTaskRequest{
			ProjectID:  current.binding.ProjectID,
			WorkflowID: &workflowID,
			Title:      title,
			LabelIDs:   labelIDs,
		})
		if createErr != nil {
			t.Fatalf("CreateTask %s: %v", title, createErr)
		}
		current.startExistingTask(t, task)
		return string(task.ID)
	}
	return currentNodeLabelFilterFixture{
		currentNodeViewFixture: current,
		alpha:                  alpha.ID.String(),
		beta:                   beta.ID.String(),
		gamma:                  gamma.ID.String(),
		taskIDs: map[string]string{
			"alpha":     startTask("alpha", alpha.ID.String()),
			"beta":      startTask("beta", beta.ID.String()),
			"both":      startTask("both", alpha.ID.String(), beta.ID.String()),
			"gamma":     startTask("gamma", gamma.ID.String()),
			"unlabeled": startTask("unlabeled"),
		},
	}
}

type currentNodeLabelFilterCase struct {
	name   string
	filter *taskpb.LabelFilter
	want   []string
}

func (f currentNodeLabelFilterFixture) exclusionCases() []currentNodeLabelFilterCase {
	named := func(mode taskpb.NamedLabelFilterMode, included []string, excluded []string) *taskpb.LabelFilter {
		return &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
			Mode:             mode,
			LabelIds:         included,
			ExcludedLabelIds: excluded,
		}},
		}
	}
	return []currentNodeLabelFilterCase{
		{
			name:   "mixed OR",
			filter: named(taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY, []string{f.gamma}, []string{f.alpha, f.beta}),
			want:   []string{f.taskIDs["alpha"], f.taskIDs["beta"], f.taskIDs["gamma"], f.taskIDs["unlabeled"]},
		},
		{
			name:   "mixed AND",
			filter: named(taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ALL, []string{f.gamma}, []string{f.alpha, f.beta}),
			want:   []string{f.taskIDs["gamma"]},
		},
		{
			name:   "excluded-only OR",
			filter: named(taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY, nil, []string{f.alpha, f.beta}),
			want:   []string{f.taskIDs["alpha"], f.taskIDs["beta"], f.taskIDs["gamma"], f.taskIDs["unlabeled"]},
		},
		{
			name:   "excluded-only AND",
			filter: named(taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ALL, nil, []string{f.alpha, f.beta}),
			want:   []string{f.taskIDs["gamma"], f.taskIDs["unlabeled"]},
		},
	}
}

func requireCurrentNodeBoardCardIDs(t *testing.T, cards []*taskpb.BoardTaskCard, want []string) {
	t.Helper()
	got := make(map[string]bool, len(cards))
	for _, card := range cards {
		got[card.TaskId] = true
	}
	if len(got) != len(want) {
		t.Fatalf("board card IDs = %v, want %v", got, want)
	}
	for _, taskID := range want {
		if !got[taskID] {
			t.Fatalf("board card IDs = %v, missing %s", got, taskID)
		}
	}
}

func TestCurrentNodeBoardLabelExclusionsFilterCountsAndCards(t *testing.T) {
	fixture := newCurrentNodeLabelFilterFixture(t)
	for _, tt := range fixture.exclusionCases() {
		t.Run(tt.name, func(t *testing.T) {
			board, err := fixture.board.Get(fixture.ctx, &taskpb.BoardGetRequest{
				ProjectId:   fixture.binding.ProjectID,
				WorkflowId:  proto.String(fixture.workflowID.String()),
				LabelFilter: tt.filter,
			})
			if err != nil {
				t.Fatalf("Board.Get: %v", err)
			}
			column := workflowViewBoardColumn(t, board, fixture.agentNodeID)
			if int(column.TaskCount) != len(tt.want) {
				t.Fatalf("agent column count = %d, want %d", column.TaskCount, len(tt.want))
			}
			page, err := fixture.board.ListNodeCards(fixture.ctx, &taskpb.BoardNodeCardsListRequest{
				ProjectId:   fixture.binding.ProjectID,
				WorkflowId:  fixture.workflowID.String(),
				NodeId:      string(fixture.agentNodeID),
				LabelFilter: tt.filter,
			})
			if err != nil {
				t.Fatalf("Board.ListNodeCards: %v", err)
			}
			requireCurrentNodeBoardCardIDs(t, page.Cards, tt.want)
		})
	}
}

func TestCurrentNodeBoardDependencyFilterCountsAndCombinesWithLabels(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	alpha, err := fixture.store.CreateProjectLabel(fixture.ctx, fixture.binding.ProjectID, "alpha")
	if err != nil {
		t.Fatalf("CreateProjectLabel: %v", err)
	}
	started := func(title string, labelIDs ...string) workflowstore.TaskRecord {
		t.Helper()
		task, createErr := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
			ProjectID:  fixture.binding.ProjectID,
			WorkflowID: &fixture.workflowID,
			Title:      title,
			LabelIDs:   labelIDs,
		})
		if createErr != nil {
			t.Fatalf("CreateTask %q: %v", title, createErr)
		}
		fixture.startExistingTask(t, task)
		return task
	}
	noDependencies := started("No dependencies", alpha.ID.String())
	satisfied := started("Satisfied", alpha.ID.String())
	unsatisfied := started("Unsatisfied", alpha.ID.String())
	otherLabel := started("Other label")
	satisfiedBlocker := createViewTask(t, fixture, "Satisfied blocker")
	unsatisfiedBlocker := createViewTask(t, fixture, "Unsatisfied blocker")
	for _, dependency := range []workflowstore.TaskDependencyAddRequest{
		{BlockerTaskID: satisfiedBlocker.ID, BlockedTaskID: satisfied.ID},
		{BlockerTaskID: unsatisfiedBlocker.ID, BlockedTaskID: unsatisfied.ID},
	} {
		if _, err := fixture.store.AddTaskDependency(fixture.ctx, dependency); err != nil {
			t.Fatalf("AddTaskDependency: %v", err)
		}
	}
	definition, _, err := fixture.store.GetDefinition(fixture.ctx, fixture.workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	if _, err := workflowfixture.MoveTask(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.ManualMoveRequest{
		TaskID:       satisfiedBlocker.ID,
		TargetNodeID: terminalNodeID(t, definition),
	}); err != nil {
		t.Fatalf("ManualMoveTask: %v", err)
	}

	labelFilter := &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
		Mode:     taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY,
		LabelIds: []string{alpha.ID.String()},
	}},
	}
	tests := []struct {
		name             string
		dependencyFilter *bool
		labelFilter      *taskpb.LabelFilter
		wantCount        int
	}{
		{name: "all", wantCount: 4},
		{name: "unblocked", dependencyFilter: boolPointerForTest(true), wantCount: 3},
		{name: "blocked", dependencyFilter: boolPointerForTest(false), wantCount: 1},
		{name: "unblocked and labels", dependencyFilter: boolPointerForTest(true), labelFilter: labelFilter, wantCount: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := tt.labelFilter
			if filter == nil {
				filter = noLabelFilter()
			}
			board, err := fixture.board.Get(fixture.ctx, &taskpb.BoardGetRequest{
				ProjectId:        fixture.binding.ProjectID,
				WorkflowId:       proto.String(fixture.workflowID.String()),
				LabelFilter:      filter,
				DependencyFilter: tt.dependencyFilter,
			})
			if err != nil {
				t.Fatalf("Board.Get: %v", err)
			}
			column := workflowViewBoardColumn(t, board, fixture.agentNodeID)
			if int(column.TaskCount) != tt.wantCount {
				t.Fatalf("agent column count = %d, want %d", column.TaskCount, tt.wantCount)
			}
		})
	}
	_ = noDependencies
	_ = unsatisfied
	_ = otherLabel
}

func boolPointerForTest(value bool) *bool {
	return &value
}
