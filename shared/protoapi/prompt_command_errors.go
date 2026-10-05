package protoapi

import (
	"fmt"

	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
)

func PromptCommandErrorToProto(err *serverapi.PromptCommandError) (proto.Message, error) {
	if validationErr := err.Validate(); validationErr != nil {
		return nil, validationErr
	}
	switch err.Kind {
	case serverapi.PromptCommandErrorKindCatalogRead:
		return &promptcommandpb.CatalogReadDetails{Command: textutil.Pointer(err.Command)}, nil
	case serverapi.PromptCommandErrorKindCommandNotFound:
		return &promptcommandpb.CommandNotFoundDetails{Command: *err.Command}, nil
	case serverapi.PromptCommandErrorKindCommandRead:
		return &promptcommandpb.CommandReadDetails{Command: *err.Command}, nil
	default:
		return nil, fmt.Errorf("invalid prompt command error kind %q", err.Kind)
	}
}

func PromptCommandErrorFromProto(details proto.Message) error {
	if err := Validate(details); err != nil {
		return err
	}
	switch selected := details.(type) {
	case *promptcommandpb.CatalogReadDetails:
		return &serverapi.PromptCommandError{Kind: serverapi.PromptCommandErrorKindCatalogRead, Command: textutil.Pointer(selected.Command)}
	case *promptcommandpb.CommandNotFoundDetails:
		return &serverapi.PromptCommandError{Kind: serverapi.PromptCommandErrorKindCommandNotFound, Command: &selected.Command}
	case *promptcommandpb.CommandReadDetails:
		return &serverapi.PromptCommandError{Kind: serverapi.PromptCommandErrorKindCommandRead, Command: &selected.Command}
	default:
		return fmt.Errorf("invalid prompt command error details %T", details)
	}
}
