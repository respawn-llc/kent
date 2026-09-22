package transport

import (
	"context"
	"database/sql"
	"errors"

	"core/server/workflowstore"
	"core/shared/apicontract"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerWorkflowGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	definitions := pb.File_kent_api_workflow_definition_workflow_definition_proto.Services().ByName("WorkflowDefinitionService")
	links := pb.File_kent_api_workflow_definition_workflow_definition_proto.Services().ByName("ProjectLinkService")
	graph := pb.File_kent_api_workflow_definition_workflow_definition_proto.Services().ByName("WorkflowGraphService")
	return errors.Join(
		registerWorkflowUnary(bindings, definitions, "Create",
			func() *pb.CreateRequest { return &pb.CreateRequest{} },
			apicontract.WorkflowService.CreateWorkflow, binaryWorkflowCreateFailure),
		registerWorkflowUnary(bindings, definitions, "CreateAndLinkProject",
			func() *pb.CreateAndLinkProjectRequest { return &pb.CreateAndLinkProjectRequest{} },
			apicontract.WorkflowService.CreateAndLinkWorkflowToProject, binaryWorkflowProjectFailure[*pb.CreateAndLinkProjectRequest]),
		registerWorkflowUnary(bindings, definitions, "Update",
			func() *pb.UpdateRequest { return &pb.UpdateRequest{} },
			apicontract.WorkflowService.UpdateWorkflow, binaryWorkflowEntityFailure[*pb.UpdateRequest]),
		registerWorkflowUnary(bindings, definitions, "List",
			func() *pb.ListRequest { return &pb.ListRequest{} },
			apicontract.WorkflowService.ListWorkflows, binaryWorkflowCreateFailure[*pb.ListRequest]),
		registerWorkflowUnary(bindings, definitions, "Get",
			func() *pb.GetRequest { return &pb.GetRequest{} },
			apicontract.WorkflowService.GetWorkflow, binaryWorkflowEntityFailure[*pb.GetRequest]),
		registerWorkflowUnary(bindings, definitions, "DeletePreview",
			func() *pb.DeletePreviewRequest { return &pb.DeletePreviewRequest{} },
			apicontract.WorkflowService.PreviewWorkflowDelete, binaryWorkflowEntityFailure[*pb.DeletePreviewRequest]),
		registerWorkflowUnary(bindings, definitions, "Delete",
			func() *pb.DeleteRequest { return &pb.DeleteRequest{} },
			apicontract.WorkflowService.DeleteWorkflow, binaryWorkflowEntityFailure[*pb.DeleteRequest]),
		registerWorkflowUnary(bindings, links, "Link",
			func() *pb.LinkProjectRequest { return &pb.LinkProjectRequest{} },
			apicontract.WorkflowService.LinkWorkflowToProject, binaryWorkflowLinkFailure[*pb.LinkProjectRequest]),
		registerWorkflowUnary(bindings, links, "List",
			func() *pb.ListProjectLinksRequest { return &pb.ListProjectLinksRequest{} },
			apicontract.WorkflowService.ListProjectWorkflowLinks, binaryWorkflowProjectFailure[*pb.ListProjectLinksRequest]),
		registerWorkflowUnary(bindings, links, "SetDefault",
			func() *pb.SetDefaultProjectLinkRequest { return &pb.SetDefaultProjectLinkRequest{} },
			apicontract.WorkflowService.SetDefaultProjectWorkflowLink, binaryWorkflowLinkFailure[*pb.SetDefaultProjectLinkRequest]),
		registerWorkflowUnary(bindings, links, "Unlink",
			func() *pb.UnlinkProjectRequest { return &pb.UnlinkProjectRequest{} },
			apicontract.WorkflowService.UnlinkWorkflowFromProject, binaryWorkflowUnlinkFailure),
		registerWorkflowUnary(bindings, definitions, "Validate",
			func() *pb.ValidateRequest { return &pb.ValidateRequest{} },
			apicontract.WorkflowService.ValidateWorkflow, binaryWorkflowEntityFailure[*pb.ValidateRequest]),
		registerWorkflowUnary(bindings, definitions, "ValidateScriptPath",
			func() *pb.ScriptPathValidateRequest { return &pb.ScriptPathValidateRequest{} },
			apicontract.WorkflowService.ValidateWorkflowScriptPath, binaryWorkflowEntityFailure[*pb.ScriptPathValidateRequest]),
		registerWorkflowUnary(bindings, graph, "ValidateDraft",
			func() *pb.GraphValidateDraftRequest { return &pb.GraphValidateDraftRequest{} },
			apicontract.WorkflowService.ValidateWorkflowGraphDraft, binaryWorkflowEntityFailure[*pb.GraphValidateDraftRequest]),
		registerWorkflowUnary(bindings, graph, "DeriveWiring",
			func() *pb.GraphDeriveWiringRequest { return &pb.GraphDeriveWiringRequest{} },
			apicontract.WorkflowService.DeriveWorkflowGraphWiring, binaryWorkflowEntityFailure[*pb.GraphDeriveWiringRequest]),
		registerWorkflowUnary(bindings, graph, "SavePreview",
			func() *pb.GraphSavePreviewRequest { return &pb.GraphSavePreviewRequest{} },
			apicontract.WorkflowService.PreviewWorkflowGraphSave, binaryWorkflowEntityFailure[*pb.GraphSavePreviewRequest]),
		registerWorkflowUnary(bindings, graph, "Save",
			func() *pb.GraphSaveRequest { return &pb.GraphSaveRequest{} },
			apicontract.WorkflowService.SaveWorkflowGraph, binaryWorkflowEntityFailure[*pb.GraphSaveRequest]),
	)
}

func registerWorkflowUnary[Request proto.Message, Success proto.Message](
	bindings map[string]gatewayBinaryBinding,
	service protoreflect.ServiceDescriptor,
	method protoreflect.Name,
	newRequest func() Request,
	invoke func(apicontract.WorkflowService, context.Context, Request) (Success, error),
	failureDetail func(Request, error) proto.Message,
	validationFailure ...func(Request, error) proto.Message,
) error {
	return registerGatewayBinaryUnary(bindings, service, method, gatewayBinaryCoreActiveOrdinary,
		newRequest, nil,
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			service := g.deps.WorkflowClient()
			if service == nil {
				var zero Success
				return zero, errors.New("workflow client is required")
			}
			return invoke(service, ctx, request)
		},
		func(_ *Gateway, _ *connectionState, request Request, err error) proto.Message {
			return failureDetail(request, err)
		}, validationFailure...)
}

func binaryWorkflowCreateFailure[Request proto.Message](_ Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	return binaryInternalFailure(err)
}

func binaryWorkflowEntityFailure[Request interface{ GetWorkflowId() string }](request Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, serverapi.ErrWorkflowNotFound) {
		return &pb.WorkflowNotFoundDetails{WorkflowId: request.GetWorkflowId()}
	}
	return binaryInternalFailure(err)
}

func binaryWorkflowProjectFailure[Request interface{ GetProjectId() string }](request Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	if errors.Is(err, serverapi.ErrProjectNotFound) {
		return &projectpb.ProjectNotFoundDetails{ProjectId: request.GetProjectId()}
	}
	return binaryInternalFailure(err)
}

func binaryWorkflowLinkFailure[Request interface {
	GetWorkflowId() string
	GetProjectId() string
}](request Request, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	if errors.Is(err, serverapi.ErrWorkflowNotFound) {
		return &pb.WorkflowNotFoundDetails{WorkflowId: request.GetWorkflowId()}
	}
	return binaryWorkflowProjectFailure(request, err)
}

func binaryWorkflowUnlinkFailure(request *pb.UnlinkProjectRequest, err error) proto.Message {
	if errors.Is(err, serverapi.ErrServerAuthRequired) {
		return &authpb.AuthRequiredDetails{}
	}
	if errors.Is(err, workflowstore.ErrReplacementDefaultInvalid) {
		return &pb.ReplacementDefaultInvalidDetails{LinkId: request.LinkId, ReplacementDefaultLinkId: request.ReplacementDefaultLinkId}
	}
	return binaryInternalFailure(err)
}
