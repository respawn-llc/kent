package taskimport

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"core/server/metadata"
	"core/server/workflowstore"
	"core/server/workflowview"
	worktree "core/server/worktreeservice"
	"core/shared/apicontract"
	"core/shared/labelcontract"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Service struct {
	metadata  *metadata.Store
	workflows apicontract.WorkflowService
	git       *worktree.GitInspector
	http      *http.Client
	lookup    func(string) (string, bool)
}

func New(store *metadata.Store, workflows apicontract.WorkflowService, git *worktree.GitInspector, client *http.Client, lookup func(string) (string, bool)) *Service {
	return &Service{metadata: store, workflows: workflows, git: git, http: client, lookup: lookup}
}

func failureDetail(err error, phase taskimportpb.FailurePhase) *taskimportpb.Failure {
	var failure *serverapi.TaskImportError
	if errors.As(err, &failure) {
		return failure.Detail
	}
	code := taskimportpb.FailureCode_FAILURE_CODE_INTERNAL
	var selection workflowstore.TaskWorkflowSelectionError
	if errors.As(err, &selection) {
		detail, projectionErr := workflowview.TaskCreateSelectionFailure(selection)
		if projectionErr == nil {
			return &taskimportpb.Failure{
				Code:  taskimportpb.FailureCode_FAILURE_CODE_WORKFLOW_SELECTION,
				Phase: phase, Diagnostic: err.Error(), WorkflowSelection: detail,
			}
		}
		err = errors.Join(err, projectionErr)
	}
	var ambiguous serverapi.WorkspaceBindingAmbiguousError
	if errors.As(err, &ambiguous) {
		code = taskimportpb.FailureCode_FAILURE_CODE_AMBIGUOUS
	}
	if errors.Is(err, serverapi.ErrProjectNotFound) || errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		code = taskimportpb.FailureCode_FAILURE_CODE_UNAVAILABLE
	}
	return &taskimportpb.Failure{Phase: phase, Code: code, Diagnostic: err.Error()}
}

func (s *Service) ImportTasks(ctx context.Context, request *taskimportpb.Request, progress serverapi.TaskImportProgressSink) (*taskimportpb.Success, error) {
	projectID, err := resolveDestination(ctx, s.metadata, request)
	if err != nil {
		return nil, &serverapi.TaskImportError{Detail: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)}
	}
	sourceWorkspace, err := resolveSourceWorkspace(ctx, s.metadata, request, projectID)
	if err != nil {
		return nil, &serverapi.TaskImportError{Detail: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)}
	}
	var explicit *repository
	if request.Repository != nil {
		value, err := parseRepository(*request.Repository)
		if err != nil {
			return nil, &serverapi.TaskImportError{Detail: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)}
		}
		explicit = &value
	}
	// URL conflicts are command errors, rejected before any item can mutate Tasks.
	for _, ref := range request.References {
		if value, ok := ref.Value.(*taskimportpb.Reference_IssueUrl); ok && explicit != nil {
			issue, err := parseIssueURL(value.IssueUrl)
			if err != nil {
				// Invalid items are reported independently by the item loop below.
				continue
			}
			if err := validateRepositorySelection(issue, explicit); err != nil {
				return nil, &serverapi.TaskImportError{Detail: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)}
			}
		}
	}
	token, warnings := acquireCredential(ctx, request.Credentials, s.lookup)
	github := newGitHubReader(s.http, token)
	account, err := github.account(ctx)
	if err != nil {
		return nil, err
	}
	if account != nil {
		progress.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Account{Account: account}})
	}
	index := labelIndex{}
	catalog, err := s.workflows.ListWorkflowProjectLabels(ctx, &workflowpb.ProjectLabelCatalogRequest{ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	for _, label := range catalog.Catalog.Labels {
		index[labelcontract.Fold(label.Name)] = label.Id
	}
	result := &taskimportpb.Success{}
	for ref := range individualReferences(request.References) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := &taskimportpb.Item{Reference: ref, ProjectId: &projectID, Warnings: warnings}
		resolved, err := s.resolveReference(ctx, request, projectID, ref, explicit)
		if err != nil {
			item.Outcome = &taskimportpb.Item_Failed{Failed: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)}
		} else {
			item.SourceUrl = proto.String(resolved.URL())
			s.importItem(ctx, request, github, account, resolved, projectID, sourceWorkspace, index, item)
		}
		switch item.Outcome.(type) {
		case *taskimportpb.Item_Created:
			result.Created++
		case *taskimportpb.Item_Skipped:
			result.Skipped++
		case *taskimportpb.Item_Failed:
			result.Failed++
		}
		progress.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Item{Item: item}})
	}
	return result, nil
}

func (s *Service) resolveReference(ctx context.Context, request *taskimportpb.Request, projectID string, ref *taskimportpb.Reference, explicit *repository) (issueReference, error) {
	if value, ok := ref.Value.(*taskimportpb.Reference_IssueUrl); ok {
		return resolveURL(value.IssueUrl, explicit)
	}
	repo, err := resolveRepository(ctx, s.metadata, s.git, request, projectID)
	return issueReference{Repository: repo, Number: ref.GetNumber()}, err
}

func (s *Service) importItem(ctx context.Context, request *taskimportpb.Request, github *githubReader, account *taskimportpb.Account, ref issueReference, projectID string, sourceWorkspace *string, index labelIndex, item *taskimportpb.Item) {
	fail := func(err error, phase taskimportpb.FailurePhase) {
		item.Outcome = &taskimportpb.Item_Failed{Failed: failureDetail(err, phase)}
	}
	if request.Mode == taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING {
		existing, err := s.metadata.FindTaskByProjectSourceURL(ctx, projectID, ref.URL())
		if err != nil {
			fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION)
			return
		}
		if existing != nil {
			item.Task = &taskimportpb.TaskIdentity{TaskId: existing.TaskID, ShortId: existing.ShortID, ProjectId: existing.ProjectID}
			item.Outcome = &taskimportpb.Item_Skipped{Skipped: &emptypb.Empty{}}
			return
		}
	}
	issue, err := github.issue(ctx, ref)
	if err != nil {
		fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_ISSUE)
		return
	}
	var labelIDs []string
	addLabel := func(candidate labelCandidate) error {
		id := candidate.match(index)
		if id == nil && request.CreateLabels {
			created, err := s.workflows.CreateWorkflowProjectLabel(ctx, &workflowpb.ProjectLabelCreateRequest{ProjectId: projectID, Name: candidate.createName})
			if err != nil {
				return err
			}
			index[labelcontract.Fold(created.Label.Name)] = created.Label.Id
			id = &created.Label.Id
		}
		if id != nil && !slices.Contains(labelIDs, *id) {
			labelIDs = append(labelIDs, *id)
		}
		return nil
	}
	for _, label := range issue.Labels {
		if err := addLabel(literalLabel(label.Name)); err != nil {
			fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_LABELS)
			return
		}
	}
	if issue.Type != nil {
		if err := addLabel(categoricalLabel("Type", issue.Type.Name)); err != nil {
			fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_LABELS)
			return
		}
	}
	if err := github.fields(ctx, ref, func(field githubField) error {
		for _, candidate := range fieldLabels(field) {
			if err := addLabel(candidate); err != nil {
				return &serverapi.TaskImportError{Detail: failureDetail(err, taskimportpb.FailurePhase_FAILURE_PHASE_LABELS)}
			}
		}
		return nil
	}); err != nil {
		fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_FIELDS)
		return
	}
	created, err := s.workflows.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId: projectID, WorkflowId: request.WorkflowId, SourceWorkspaceId: sourceWorkspace,
		Title: issue.Title, Body: proto.String(importedBody(issue, ref, account, time.Now())), SourceUrl: proto.String(ref.URL()), LabelIds: labelIDs,
	})
	if err != nil {
		var committed *serverapi.CommittedTaskCreationError
		if errors.As(err, &committed) {
			item.Task = &taskimportpb.TaskIdentity{TaskId: committed.TaskID, ShortId: committed.ShortID, ProjectId: committed.ProjectID}
		}
		fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_CREATION)
		return
	}
	item.Task = &taskimportpb.TaskIdentity{TaskId: created.Task.Id, ShortId: created.Task.ShortId, ProjectId: created.Task.ProjectId}
	if err := github.comments(ctx, ref, func(comment githubComment) error {
		_, err := s.workflows.AddWorkflowTaskComment(ctx, &taskpb.CommentAddRequest{TaskId: created.Task.Id, Body: comment.Body, Author: taskpb.CommentAuthorKind_COMMENT_AUTHOR_KIND_USER, AuthorId: &comment.User.Login})
		return err
	}); err != nil {
		fail(err, taskimportpb.FailurePhase_FAILURE_PHASE_COMMENTS)
		return
	}
	item.Outcome = &taskimportpb.Item_Created{Created: &emptypb.Empty{}}
}
