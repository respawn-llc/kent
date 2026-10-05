package client

import (
	"context"

	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func sessionRuntimeMethod(name protoreflect.Name) protoreflect.MethodDescriptor {
	return bootstrapMethod(sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto, "SessionRuntimeService", name)
}

func (c *Remote) ActivateSessionRuntime(ctx context.Context, request serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeAttachment, error) {
	generated, err := protoapi.SessionRuntimeActivateToProto(request)
	if err != nil {
		return serverapi.SessionRuntimeAttachment{}, err
	}
	success, err := callGeneratedBinary(c, ctx, sessionRuntimeMethod("Activate"), generated,
		&sessionlaunchpb.SessionRuntimeActivateResult{}, sessionRuntimeError[*sessionlaunchpb.SessionRuntimeActivateError])
	if err != nil {
		return serverapi.SessionRuntimeAttachment{}, err
	}
	attachment := protoapi.SessionRuntimeAttachmentFromProto(success.Attachment)
	return attachment, attachment.ValidateForSession(request.SessionID)
}

func (c *Remote) ReleaseSessionRuntime(ctx context.Context, request serverapi.SessionRuntimeReleaseRequest) (*sessionlaunchpb.SessionRuntimeReleaseSuccess, error) {
	generated, err := protoapi.SessionRuntimeReleaseToProto(request)
	if err != nil {
		return nil, err
	}
	return callGeneratedBinary(c, ctx, sessionRuntimeMethod("Release"), generated,
		&sessionlaunchpb.SessionRuntimeReleaseResult{}, sessionRuntimeError[*sessionlaunchpb.SessionRuntimeReleaseError])
}

func sessionRuntimeError[Failure interface {
	GetCode() string
	GetRuntimeUnavailable() *sessionlaunchpb.SessionRuntimeUnavailableDetails
}](failure Failure) error {
	if failure.GetRuntimeUnavailable() != nil {
		return serverapi.ErrRuntimeUnavailable
	}
	return generatedOperationFailure(failure.GetCode())
}
