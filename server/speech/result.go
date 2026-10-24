package speech

import (
	"errors"
	"unicode"

	"core/shared/config"
	speechpb "core/shared/protoapi/gen/kent/api/speech"
	"google.golang.org/protobuf/types/known/emptypb"
)

func Normalize(text string) *speechpb.UploadResult {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return &speechpb.UploadResult{Outcome: &speechpb.UploadResult_Transcript{Transcript: text}}
		}
	}
	return &speechpb.UploadResult{Outcome: &speechpb.UploadResult_NoOp{NoOp: &emptypb.Empty{}}}
}

func Failure(kind speechpb.FailureKind, connection *config.ConnectionID, err error) *speechpb.UploadResult {
	failure := &speechpb.Failure{Kind: kind, Diagnostic: err.Error()}
	var rejection *providerRejection
	if errors.As(err, &rejection) {
		status := uint32(rejection.status)
		failure.HttpStatus = &status
	}
	if connection != nil {
		id := string(*connection)
		failure.ConnectionId = &id
	}
	return &speechpb.UploadResult{Outcome: &speechpb.UploadResult_Failure{Failure: failure}}
}
