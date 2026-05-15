package workflowview

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"builder/server/metadata"
	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"builder/server/workflowjson"
	"builder/shared/serverapi"
)

type Service struct {
	metadata *metadata.Store
	queries  *sqlitegen.Queries
}

func New(metadataStore *metadata.Store) (*Service, error) {
	if metadataStore == nil || metadataStore.Queries() == nil {
		return nil, errors.New("metadata store is required")
	}
	return &Service{metadata: metadataStore, queries: metadataStore.Queries()}, nil
}

func (s *Service) GetDefinition(ctx context.Context, workflowID string) (serverapi.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	if s == nil {
		return serverapi.WorkflowDefinition{}, nil, errors.New("workflow view service is required")
	}
	if strings.TrimSpace(workflowID) == "" {
		return serverapi.WorkflowDefinition{}, nil, errors.New("workflow_id is required")
	}
	return s.definition(ctx, workflowID)
}

func (s *Service) GetBoard(ctx context.Context, projectID string) (serverapi.WorkflowBoard, error) {
	if s == nil {
		return serverapi.WorkflowBoard{}, errors.New("workflow view service is required")
	}
	if strings.TrimSpace(projectID) == "" {
		return serverapi.WorkflowBoard{}, errors.New("project_id is required")
	}
	links, err := s.queries.ListProjectWorkflowLinks(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	tasks, err := s.queries.ListTasksByProject(ctx, projectID)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	placementsByTaskID, err := s.boardPlacementsByTask(ctx, tasks)
	if err != nil {
		return serverapi.WorkflowBoard{}, err
	}
	workflowIDs := make([]string, 0, len(links)+len(tasks))
	seen := map[string]bool{}
	for _, link := range links {
		if !seen[link.WorkflowID] {
			workflowIDs = append(workflowIDs, link.WorkflowID)
			seen[link.WorkflowID] = true
		}
	}
	for _, task := range tasks {
		if !seen[task.WorkflowID] {
			workflowIDs = append(workflowIDs, task.WorkflowID)
			seen[task.WorkflowID] = true
		}
	}
	board := serverapi.WorkflowBoard{ProjectID: projectID}
	for _, workflowID := range workflowIDs {
		def, nodeKinds, err := s.definition(ctx, workflowID)
		if err != nil {
			return serverapi.WorkflowBoard{}, err
		}
		wfBoard := serverapi.WorkflowBoardWorkflow{Workflow: def.Workflow}
		for _, node := range def.Nodes {
			wfBoard.Nodes = append(wfBoard.Nodes, serverapi.WorkflowBoardNode{Node: node})
		}
		nodeIndex := map[string]int{}
		for index, node := range wfBoard.Nodes {
			nodeIndex[node.Node.ID] = index
		}
		for _, task := range tasks {
			if task.WorkflowID != workflowID {
				continue
			}
			placements := placementsByTaskID[task.ID]
			summary := taskSummary(task, placements, nodeKinds)
			wfBoard.Tasks = append(wfBoard.Tasks, summary)
			for _, placement := range placements {
				if placement.State != "active" && placement.State != "waiting_approval" {
					continue
				}
				index, ok := nodeIndex[placement.NodeID]
				if !ok {
					continue
				}
				dto := placementDTO(placement)
				wfBoard.Nodes[index].ActivePlacements = append(wfBoard.Nodes[index].ActivePlacements, dto)
				wfBoard.Nodes[index].Tasks = append(wfBoard.Nodes[index].Tasks, summary)
			}
		}
		board.Workflows = append(board.Workflows, wfBoard)
	}
	return board, nil
}

func (s *Service) boardPlacementsByTask(ctx context.Context, tasks []sqlitegen.Task) (map[string][]sqlitegen.TaskNodePlacement, error) {
	if len(tasks) == 0 {
		return map[string][]sqlitegen.TaskNodePlacement{}, nil
	}
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	placements, err := s.queries.ListTaskNodePlacementsByTasks(ctx, taskIDs)
	if err != nil {
		return nil, err
	}
	byTaskID := make(map[string][]sqlitegen.TaskNodePlacement, len(tasks))
	for _, placement := range placements {
		byTaskID[placement.TaskID] = append(byTaskID[placement.TaskID], placement)
	}
	return byTaskID, nil
}

func (s *Service) GetTask(ctx context.Context, taskID string) (serverapi.WorkflowTaskDetail, error) {
	if s == nil {
		return serverapi.WorkflowTaskDetail{}, errors.New("workflow view service is required")
	}
	if strings.TrimSpace(taskID) == "" {
		return serverapi.WorkflowTaskDetail{}, errors.New("task_id is required")
	}
	task, err := s.queries.GetTask(ctx, taskID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	_, nodeKinds, err := s.definition(ctx, task.WorkflowID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return serverapi.WorkflowTaskDetail{}, err
	}
	placements, err := s.queries.ListTaskNodePlacements(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	runs, err := s.queries.ListTaskRuns(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	transitions, err := s.queries.ListTaskTransitions(ctx, task.ID)
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	comments, err := s.queries.ListTaskComments(ctx, sqlitegen.ListTaskCommentsParams{TaskID: task.ID, IncludeDeleted: int64(0)})
	if err != nil {
		return serverapi.WorkflowTaskDetail{}, err
	}
	detail := serverapi.WorkflowTaskDetail{Summary: taskSummary(task, placements, nodeKinds)}
	for _, placement := range placements {
		detail.Placements = append(detail.Placements, placementDTO(placement))
	}
	for _, run := range runs {
		detail.Runs = append(detail.Runs, serverapi.WorkflowRun{ID: run.ID, TaskID: run.TaskID, PlacementID: run.PlacementID, NodeID: run.NodeID, SessionID: run.SessionID.String, Generation: run.RunGeneration, StartedAtUnixMs: run.StartedAtUnixMs, CompletedAtUnixMs: run.CompletedAtUnixMs, InterruptedAtUnixMs: run.InterruptedAtUnixMs, InterruptionReason: run.InterruptionReason})
	}
	for _, transition := range transitions {
		outputs := map[string]string{}
		if err := workflowjson.UnmarshalString(transition.OutputValuesJson, &outputs); err != nil {
			return serverapi.WorkflowTaskDetail{}, err
		}
		dto := serverapi.WorkflowTaskTransition{ID: transition.ID, TaskID: transition.TaskID, TransitionID: transition.TransitionID, State: transition.State, Commentary: transition.Commentary, OutputValues: outputs, CreatedAt: transition.CreatedAtUnixMs}
		edges, err := s.queries.ListTaskTransitionEdges(ctx, transition.ID)
		if err != nil {
			return serverapi.WorkflowTaskDetail{}, err
		}
		for _, edge := range edges {
			dto.Edges = append(dto.Edges, serverapi.WorkflowTransitionEdge{ID: edge.ID, TaskTransitionID: edge.TaskTransitionID, WorkflowEdgeID: edge.WorkflowEdgeID.String, EdgeKey: edge.EdgeKey, TargetNodeID: edge.TargetNodeID.String, TargetPlacementID: edge.TargetPlacementID.String, State: edge.State, WorkflowRevisionSeen: edge.WorkflowRevisionSeen})
		}
		detail.Transitions = append(detail.Transitions, dto)
	}
	for _, comment := range comments {
		detail.Comments = append(detail.Comments, commentDTO(comment))
	}
	return detail, nil
}

func (s *Service) definition(ctx context.Context, workflowID string) (serverapi.WorkflowDefinition, map[string]workflow.NodeKind, error) {
	row, err := s.queries.GetWorkflow(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	nodes, err := s.queries.ListWorkflowNodes(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	groups, err := s.queries.ListWorkflowTransitionGroups(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	edges, err := s.queries.ListWorkflowEdges(ctx, workflowID)
	if err != nil {
		return serverapi.WorkflowDefinition{}, nil, err
	}
	def := serverapi.WorkflowDefinition{Workflow: serverapi.WorkflowRecord{ID: row.ID, Name: row.Name, Description: row.Description, GraphRevision: row.GraphRevision}}
	nodeKinds := map[string]workflow.NodeKind{}
	for _, node := range nodes {
		fields := []serverapi.WorkflowOutputField{}
		if err := workflowjson.UnmarshalString(node.OutputFieldsJson, &fields); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		def.Nodes = append(def.Nodes, serverapi.WorkflowNode{ID: node.ID, WorkflowID: node.WorkflowID, Key: node.NodeKey, Kind: node.Kind, DisplayName: node.DisplayName, SubagentRole: node.SubagentRole, PromptTemplate: node.PromptTemplate, OutputFields: fields})
		nodeKinds[node.ID] = workflow.NodeKind(node.Kind)
	}
	for _, group := range groups {
		def.TransitionGroups = append(def.TransitionGroups, serverapi.WorkflowTransitionGroup{ID: group.ID, WorkflowID: group.WorkflowID, SourceNodeID: group.SourceNodeID, TransitionID: string(group.TransitionID), DisplayName: group.DisplayName})
	}
	for _, edge := range edges {
		inputs := []serverapi.WorkflowInputBinding{}
		requirements := []serverapi.WorkflowOutputRequirement{}
		if err := workflowjson.UnmarshalString(edge.InputBindingsJson, &inputs); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		if err := workflowjson.UnmarshalString(edge.OutputRequirementsJson, &requirements); err != nil {
			return serverapi.WorkflowDefinition{}, nil, err
		}
		def.Edges = append(def.Edges, serverapi.WorkflowEdge{ID: edge.ID, WorkflowID: edge.WorkflowID, TransitionGroupID: edge.TransitionGroupID, Key: edge.EdgeKey, TargetNodeID: edge.TargetNodeID, RequiresApproval: edge.RequiresApproval != 0, ContextMode: edge.ContextMode, InputBindings: inputs, OutputRequirements: requirements})
	}
	return def, nodeKinds, nil
}

func taskSummary(task sqlitegen.Task, placements []sqlitegen.TaskNodePlacement, nodeKinds map[string]workflow.NodeKind) serverapi.WorkflowTaskSummary {
	summary := serverapi.WorkflowTaskSummary{ID: task.ID, ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, ShortID: task.ShortID, Title: task.Title, CanceledAt: task.CanceledAtUnixMs, CancelReason: task.CancellationReason}
	seenActive := map[string]bool{}
	for _, placement := range placements {
		if placement.State != "active" && placement.State != "waiting_approval" {
			continue
		}
		if nodeKinds[placement.NodeID] == workflow.NodeKindTerminal {
			summary.Done = true
		}
		if !seenActive[placement.NodeID] {
			summary.ActiveNodeIDs = append(summary.ActiveNodeIDs, placement.NodeID)
			seenActive[placement.NodeID] = true
		}
	}
	return summary
}

func placementDTO(placement sqlitegen.TaskNodePlacement) serverapi.WorkflowPlacement {
	return serverapi.WorkflowPlacement{ID: placement.ID, TaskID: placement.TaskID, NodeID: placement.NodeID, State: placement.State}
}

func commentDTO(comment sqlitegen.TaskComment) serverapi.WorkflowTaskComment {
	return serverapi.WorkflowTaskComment{ID: comment.ID, TaskID: comment.TaskID, Body: comment.Body, Author: comment.AuthorKind, AuthorID: comment.AuthorID, DeletedAt: comment.DeletedAtUnixMs, UpdatedAt: comment.UpdatedAtUnixMs}
}
