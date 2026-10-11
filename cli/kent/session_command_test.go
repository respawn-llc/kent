package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"core/shared/client"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/sessionenv"
)

type sessionCommandTestRemote struct {
	archiveResponse *sessionlaunchpb.SessionArchiveSuccess
	archiveErr      error
	deleteResponse  *sessionlaunchpb.SessionDeleteSuccess
	deleteErr       error
	archiveRequests []*sessionlaunchpb.SessionArchiveRequest
	deleteRequests  []*sessionlaunchpb.SessionDeleteRequest
	archiveDeadline bool
}

func (r *sessionCommandTestRemote) ArchiveSession(
	ctx context.Context,
	request *sessionlaunchpb.SessionArchiveRequest,
) (*sessionlaunchpb.SessionArchiveSuccess, error) {
	r.archiveRequests = append(r.archiveRequests, request)
	_, r.archiveDeadline = ctx.Deadline()
	return r.archiveResponse, r.archiveErr
}

func (r *sessionCommandTestRemote) DeleteSession(
	_ context.Context,
	request *sessionlaunchpb.SessionDeleteRequest,
) (*sessionlaunchpb.SessionDeleteSuccess, error) {
	r.deleteRequests = append(r.deleteRequests, request)
	return r.deleteResponse, r.deleteErr
}

func (*sessionCommandTestRemote) Close() error { return nil }

func TestSessionArchiveCommandSuccess(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	remote := &sessionCommandTestRemote{
		archiveResponse: &sessionlaunchpb.SessionArchiveSuccess{
			SessionId:  "33333333-3333-4333-8333-333333333333",
			OutputPath: "/tmp/session-archive.tar.zst",
		},
	}
	dialDeadline := false
	command := sessionCommand{
		openRemote: func(ctx context.Context) (sessionCommandRemote, error) {
			_, dialDeadline = ctx.Deadline()
			return remote, nil
		},
	}
	exitCode, stdout, stderr := runSessionCommandTest(t, command, []string{"archive", "0007", "--output", "/tmp/session-archive.tar.zst", "--json"})

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr)
	}
	if !dialDeadline || remote.archiveDeadline {
		t.Fatalf("contexts = dial deadline %t, archive deadline %t; want bounded dial and unbounded archive", dialDeadline, remote.archiveDeadline)
	}
	if len(remote.archiveRequests) != 1 ||
		remote.archiveRequests[0].SessionId.GetShortId() != 7 ||
		remote.archiveRequests[0].OutputPath != "/tmp/session-archive.tar.zst" {
		t.Fatalf("archive requests = %+v", remote.archiveRequests)
	}
	var envelope sessionRemovalJSONEnvelope
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if envelope.Status != "ok" || envelope.Result == nil ||
		envelope.Result.SessionID != "33333333-3333-4333-8333-333333333333" ||
		envelope.Result.OutputPath == nil ||
		*envelope.Result.OutputPath != "/tmp/session-archive.tar.zst" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestSessionDeleteCommandSuccess(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	remote := &sessionCommandTestRemote{
		deleteResponse: &sessionlaunchpb.SessionDeleteSuccess{SessionId: "33333333-3333-4333-8333-333333333333"},
	}
	command := sessionCommand{
		openRemote: func(context.Context) (sessionCommandRemote, error) {
			return remote, nil
		},
	}
	exitCode, stdout, stderr := runSessionCommandTest(t, command, []string{"delete", "7", "--confirm"})

	if exitCode != 0 || stderr != "" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout, stderr)
	}
	if len(remote.deleteRequests) != 1 || remote.deleteRequests[0].SessionId.GetShortId() != 7 {
		t.Fatalf("delete requests = %+v", remote.deleteRequests)
	}
}

func TestSessionRemovalCommandConfirmationAndServerSelfGuard(t *testing.T) {
	const ownSessionID = "33333333-3333-4333-8333-333333333333"
	tests := []struct {
		name      string
		env       string
		args      []string
		wantCode  string
		wantDials int
	}{
		{name: "archive self target", env: ownSessionID, args: []string{"archive", "7", "--output", "/tmp/own.tar.zst", "--json"}, wantCode: "self_session_forbidden", wantDials: 1},
		{name: "delete self target", env: ownSessionID, args: []string{"delete", "7", "--confirm", "--json"}, wantCode: "self_session_forbidden", wantDials: 1},
		{name: "delete confirmation", args: []string{"delete", "7", "--json"}, wantCode: "confirmation_required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(sessionenv.SessionIDEnv, test.env)
			dials := 0
			command := sessionCommand{
				openRemote: func(context.Context) (sessionCommandRemote, error) {
					dials++
					detail := &sharedpb.SessionSelfTargetDetails{SessionId: ownSessionID}
					return &sessionCommandTestRemote{
						archiveErr: &client.SessionArchiveFailureError{Failure: &sessionlaunchpb.SessionArchiveError{Code: "self_target", Detail: &sessionlaunchpb.SessionArchiveError_SelfTarget{SelfTarget: detail}}},
						deleteErr:  &client.SessionDeleteFailureError{Failure: &sessionlaunchpb.SessionDeleteError{Code: "self_target", Detail: &sessionlaunchpb.SessionDeleteError_SelfTarget{SelfTarget: detail}}},
					}, nil
				},
			}
			exitCode, stdout, stderr := runSessionCommandTest(t, command, test.args)
			if exitCode != 1 {
				t.Fatalf("exit = %d, want 1; stderr = %q", exitCode, stderr)
			}
			if dials != test.wantDials {
				t.Fatalf("dials = %d, want %d", dials, test.wantDials)
			}
			var envelope sessionRemovalJSONEnvelope
			if err := json.Unmarshal(stdout, &envelope); err != nil {
				t.Fatalf("decode JSON: %v", err)
			}
			if envelope.Error == nil || envelope.Error.Code != test.wantCode {
				t.Fatalf("envelope = %+v, want %q", envelope, test.wantCode)
			}
		})
	}
}

func TestSessionRemovalFailureClassification(t *testing.T) {
	sessionID := "33333333-3333-4333-8333-333333333333"
	outputPath := "/tmp/session-failure.tar.zst"
	notFound := &sessionlaunchpb.SessionArchiveError_SessionNotFound{SessionNotFound: &sessionlaunchpb.SessionNotFoundDetails{SessionId: &sessionID}}
	inUse := &sessionlaunchpb.SessionDeleteError_SessionInUse{SessionInUse: &sessionlaunchpb.SessionInUseDetails{SessionId: sessionID}}
	invalidPath := &sessionlaunchpb.SessionArchiveError_InvalidOutputPath{InvalidOutputPath: &sessionlaunchpb.InvalidArchiveOutputPathDetails{Path: outputPath, Reason: sessionlaunchpb.InvalidArchiveOutputPathReason_INVALID_ARCHIVE_OUTPUT_PATH_REASON_NOT_ABSOLUTE}}
	outputExists := &sessionlaunchpb.SessionArchiveError_OutputExists{OutputExists: &sessionlaunchpb.ArchiveOutputExistsDetails{Path: outputPath}}
	tests := []struct {
		name        string
		err         error
		archive     bool
		wantCode    string
		wantMessage string
	}{
		{name: "session not found", err: archiveWireFailure("session_not_found", notFound), archive: true, wantCode: "session_not_found"},
		{name: "session in use", err: deleteWireFailure("session_in_use", inUse), wantCode: "session_in_use"},
		{name: "invalid output path", err: archiveWireFailure("invalid_output_path", invalidPath), archive: true, wantCode: "invalid_output_path"},
		{name: "output exists", err: archiveWireFailure("output_exists", outputExists), archive: true, wantCode: "output_exists"},
		{name: "server failure", err: errors.New("server-authored cleanup failure"), archive: true, wantCode: "request_failed", wantMessage: "server-authored cleanup failure"},
		{name: "transport", err: errors.New("connection closed"), archive: true, wantCode: "request_failed", wantMessage: "connection closed"},
		{name: "unknown future wire error", err: archiveWireFailure("future_server_failure", nil), archive: true, wantCode: "request_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var failure *sessionRemovalError
			if test.archive {
				failure = sessionArchiveFailure(sessionID, test.err)
			} else {
				failure = sessionDeleteFailure(sessionID, test.err)
			}
			if failure == nil || failure.Code != test.wantCode {
				t.Fatalf("failure = %+v, want code %q message %q", failure, test.wantCode, test.wantMessage)
			}
			if test.wantMessage != "" && failure.Message != test.wantMessage {
				t.Fatalf("failure message = %q, want server error %q", failure.Message, test.wantMessage)
			}
		})
	}
}

func TestSessionRemovalMissingNumericJSONOmitsUUID(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	command := sessionCommand{openRemote: func(context.Context) (sessionCommandRemote, error) {
		return &sessionCommandTestRemote{deleteErr: deleteWireFailure("session_not_found",
			&sessionlaunchpb.SessionDeleteError_SessionNotFound{SessionNotFound: &sessionlaunchpb.SessionNotFoundDetails{}})}, nil
	}}
	code, output, stderr := runSessionCommandTest(t, command, []string{"delete", "7", "--confirm", "--json"})
	if code != 1 || stderr != "" {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	var envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatal(err)
	}
	if _, exists := envelope.Error["session_id"]; exists {
		t.Fatalf("unknown UUID was included: %s", output)
	}
}

func runSessionCommandTest(t *testing.T, command sessionCommand, args []string) (int, []byte, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exitCode := command.run(args, &stdout, &stderr)
	return exitCode, stdout.Bytes(), stderr.String()
}

func archiveWireFailure(code string, detail any) error {
	failure := &sessionlaunchpb.SessionArchiveError{Code: code}
	switch detail := detail.(type) {
	case nil:
	case *sessionlaunchpb.SessionArchiveError_SessionNotFound:
		failure.Detail = detail
	case *sessionlaunchpb.SessionArchiveError_SessionInUse:
		failure.Detail = detail
	case *sessionlaunchpb.SessionArchiveError_InvalidOutputPath:
		failure.Detail = detail
	case *sessionlaunchpb.SessionArchiveError_OutputExists:
		failure.Detail = detail
	default:
		panic("unsupported Session archive test detail")
	}
	return &client.SessionArchiveFailureError{Failure: failure}
}

func deleteWireFailure(code string, detail any) error {
	failure := &sessionlaunchpb.SessionDeleteError{Code: code}
	switch detail := detail.(type) {
	case nil:
	case *sessionlaunchpb.SessionDeleteError_SessionNotFound:
		failure.Detail = detail
	case *sessionlaunchpb.SessionDeleteError_SessionInUse:
		failure.Detail = detail
	default:
		panic("unsupported Session delete test detail")
	}
	return &client.SessionDeleteFailureError{Failure: failure}
}
