package workflowsvc

import (
	"context"
	"errors"

	"core/server/promptcontrol"
	"core/server/workflow"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"
)

type WorkflowDefinitionReadModel interface {
	GetDefinition(context.Context, runtimeids.WorkflowID) (*pb.WorkflowDefinition, map[string]workflow.NodeKind, error)
}

type WorkflowBoardReadModel interface {
	Get(context.Context, *taskpb.BoardGetRequest) (*taskpb.Board, error)
	ListNodeCards(context.Context, *taskpb.BoardNodeCardsListRequest) (*taskpb.BoardNodeCardsListSuccess, error)
}

type WorkflowTaskListReadModel interface {
	List(context.Context, *taskpb.ListRequest) (*taskpb.ListSuccess, error)
	CountGroups(context.Context, *taskpb.ProjectTaskGroupCountsRequest) (*taskpb.ProjectTaskGroupCountsSuccess, error)
}

type WorkflowTaskSearchReadModel interface {
	Search(context.Context, *taskpb.SearchRequest) (*taskpb.SearchSuccess, error)
}

type WorkflowTaskDetailReadModel interface {
	GetTask(context.Context, string) (*taskpb.TaskDetail, error)
	GetTaskByProjectShortID(context.Context, string, string) (*taskpb.TaskDetail, error)
	GetTaskByShortID(context.Context, string) (*taskpb.TaskDetail, error)
	ListCurrentNodes(context.Context, string) ([]workflow.CurrentNode, error)
}

type WorkflowTaskDependencyReadModel interface {
	GetTaskDependencies(context.Context, string) (*taskpb.TaskDependencies, error)
	CountUnsatisfiedBlockers(context.Context, string) (int, error)
	ListTaskDependencies(context.Context, string, *taskpb.DependencyDirection) (*taskpb.DependencyListSuccess, error)
}

type WorkflowActivityReadModel interface {
	List(context.Context, *taskpb.TaskOffsetPageRequest) (*taskpb.ActivityListSuccess, error)
}

type WorkflowTaskSessionReadModel interface {
	List(context.Context, *taskpb.TaskOffsetPageRequest) (*taskpb.SessionListSuccess, error)
}

type WorkflowAttentionReadModel interface {
	List(context.Context, *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error)
	ListTask(context.Context, *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error)
}

type ReadModels struct {
	Definitions      WorkflowDefinitionReadModel
	Board            WorkflowBoardReadModel
	TaskList         WorkflowTaskListReadModel
	TaskSearch       WorkflowTaskSearchReadModel
	TaskDetail       WorkflowTaskDetailReadModel
	TaskDependencies WorkflowTaskDependencyReadModel
	TaskSessions     WorkflowTaskSessionReadModel
	Activity         WorkflowActivityReadModel
	Attention        WorkflowAttentionReadModel
	PendingPrompts   promptcontrol.PendingPromptSource
}

func (r ReadModels) validate() error {
	switch {
	case r.Definitions == nil:
		return errors.New("workflow definition read model is required")
	case r.Board == nil:
		return errors.New("workflow board read model is required")
	case r.TaskList == nil:
		return errors.New("workflow task list read model is required")
	case r.TaskSearch == nil:
		return errors.New("workflow task search read model is required")
	case r.TaskDetail == nil:
		return errors.New("workflow task detail read model is required")
	case r.TaskDependencies == nil:
		return errors.New("workflow task dependency read model is required")
	case r.TaskSessions == nil:
		return errors.New("workflow Task Session read model is required")
	case r.Activity == nil:
		return errors.New("workflow activity read model is required")
	case r.Attention == nil:
		return errors.New("workflow attention read model is required")
	case r.PendingPrompts == nil:
		return errors.New("workflow pending prompt source is required")
	default:
		return nil
	}
}
