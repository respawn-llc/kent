package speech

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"

	"core/server/authservice"
	"core/server/launch"
	"core/server/llm"
	"core/shared/config"
	speechpb "core/shared/protoapi/gen/kent/api/speech"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Service struct {
	root        string
	rotation    *launch.ConnectionRotation
	connections *authservice.ConnectionResolver
}

func (s *Service) Transcribe(ctx context.Context, contentType string, input io.ReadCloser) *speechpb.UploadResult {
	selection, err := s.selection()
	if err != nil {
		return Failure(speechpb.FailureKind_FAILURE_KIND_CONNECTION, nil, err)
	}
	if selection.Limits == nil {
		return Failure(speechpb.FailureKind_FAILURE_KIND_UNAVAILABLE, nil, errors.New("no speech-capable default connection"))
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Failure(speechpb.FailureKind_FAILURE_KIND_INPUT, nil, err)
	}
	index := slices.IndexFunc(selection.Limits.Encodings, func(encoding llm.SpeechEncoding) bool {
		return encoding.MIMEType == mediaType
	})
	if index < 0 {
		return Failure(speechpb.FailureKind_FAILURE_KIND_INPUT, nil, errors.New("unsupported audio encoding"))
	}
	invocation, err := selection.Allocate(s.rotation)
	if err != nil {
		return Failure(speechpb.FailureKind_FAILURE_KIND_CONNECTION, nil, err)
	}
	credential, err := s.connections.ResolveDefinitionAuth(ctx, invocation.ID, invocation.Definition)
	if err != nil {
		return Failure(speechpb.FailureKind_FAILURE_KIND_CONNECTION, &invocation.ID, err)
	}
	bounded := http.MaxBytesReader(nil, input, selection.Limits.MaxBytes)
	text, err := transcribe(ctx, invocation, credential, selection.Limits.Encodings[index], bounded)
	if err == nil {
		return Normalize(text)
	}
	kind := speechpb.FailureKind_FAILURE_KIND_PROVIDER
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		kind = speechpb.FailureKind_FAILURE_KIND_SIZE_LIMIT
	}
	return Failure(kind, &invocation.ID, err)
}

func NewService(root string, rotation *launch.ConnectionRotation, connections *authservice.ConnectionResolver) *Service {
	return &Service{root: root, rotation: rotation, connections: connections}
}

func (s *Service) selection() (Selection, error) {
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: s.root})
	if err != nil {
		return Selection{}, err
	}
	return EligibleDefaults(app.Settings)
}

func (s *Service) GetAvailability(_ context.Context, _ *emptypb.Empty) (*speechpb.Availability, error) {
	selection, err := s.selection()
	if err != nil {
		return nil, err
	}
	if selection.Limits == nil {
		return &speechpb.Availability{State: &speechpb.Availability_Unavailable{Unavailable: &emptypb.Empty{}}}, nil
	}
	limits := selection.Limits
	encodings := make([]*speechpb.Encoding, 0, len(limits.Encodings))
	for _, encoding := range limits.Encodings {
		encodings = append(encodings, &speechpb.Encoding{MimeType: encoding.MIMEType, Extension: encoding.Extension})
	}
	return &speechpb.Availability{State: &speechpb.Availability_Available{Available: &speechpb.Available{
		Encodings: encodings, MaxBytes: uint32(limits.MaxBytes), MaxDuration: durationpb.New(limits.MaxDuration),
	}}}, nil
}
