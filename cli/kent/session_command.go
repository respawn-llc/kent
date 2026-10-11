package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"core/cli/app"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
)

const selfSessionRemovalMessage = "You're trying to delete your own session, which is effectively a suicide. Don't do it, you still have things worth living for! Seek help immediately via ask_question, or exclude your session if this is accidental"

type sessionCommandRemote interface {
	ArchiveSession(context.Context, *sessionlaunchpb.SessionArchiveRequest) (*sessionlaunchpb.SessionArchiveSuccess, error)
	DeleteSession(context.Context, *sessionlaunchpb.SessionDeleteRequest) (*sessionlaunchpb.SessionDeleteSuccess, error)
	Close() error
}

type sessionCommandRemoteOpener func(context.Context) (sessionCommandRemote, error)

type sessionCommand struct {
	openRemote sessionCommandRemoteOpener
}

type sessionRemovalResult struct {
	SessionID  string  `json:"session_id"`
	OutputPath *string `json:"output_path,omitempty"`
}

type sessionRemovalError struct {
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	SessionID *string `json:"session_id,omitempty"`
}

type sessionRemovalJSONEnvelope struct {
	Status string                `json:"status"`
	Result *sessionRemovalResult `json:"result,omitempty"`
	Error  *sessionRemovalError  `json:"error,omitempty"`
}

func sessionSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return sessionCommand{openRemote: openSessionCommandRemote}.run(args, stdout, stderr)
}

func (c sessionCommand) run(args []string, stdout io.Writer, stderr io.Writer) int {
	return dispatchCommandGroup(args, stdout, stderr, commandGroup{
		path:  "session",
		usage: sessionUsage,
		routes: map[string]commandHandler{
			"archive": c.archive,
			"delete":  c.delete,
		},
	})
}

func (c sessionCommand) archive(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" session archive", stderr, sessionArchiveUsage)
	outputPath := fs.String("output", "", "absolute server-local .tar.zst output path")
	jsonOut := fs.Bool("json", false, "write a stable JSON envelope")
	positionals, ok, exitCode := parseInterspersedPositionals(fs, args)
	if !ok {
		return exitCode
	}
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "session archive requires <session-id>")
		return 2
	}
	selector, err := runtimeids.ParseSessionSelector(positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	sessionID := selector.String()
	if strings.TrimSpace(*outputPath) == "" {
		fmt.Fprintln(stderr, "session archive requires --output <path>")
		return 2
	}
	remote, err := c.dial()
	if err != nil {
		return writeSessionRemovalOutcome(
			stdout,
			stderr,
			nil,
			sessionArchiveFailure(sessionID, err),
			*jsonOut,
		)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result, err := runSessionArchiveUseCase(ctx, remote, sessionID, *outputPath)
	stop()
	var failure *sessionRemovalError
	if err != nil {
		failure = sessionArchiveFailure(sessionID, err)
	}
	exitCode = writeSessionRemovalOutcome(stdout, stderr, result, failure, *jsonOut)
	if closeErr := remote.Close(); closeErr != nil {
		fmt.Fprintf(stderr, "close Session archive remote: %v\n", closeErr)
	}
	return exitCode
}

func (c sessionCommand) delete(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" session delete", stderr, sessionDeleteUsage)
	confirmed := fs.Bool("confirm", false, "confirm Session deletion")
	jsonOut := fs.Bool("json", false, "write a stable JSON envelope")
	positionals, ok, exitCode := parseInterspersedPositionals(fs, args)
	if !ok {
		return exitCode
	}
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "session delete requires <session-id>")
		return 2
	}
	selector, err := runtimeids.ParseSessionSelector(positionals[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	sessionID := selector.String()
	if !*confirmed {
		return writeSessionRemovalOutcome(stdout, stderr, nil, sessionRemovalFailure(
			sessionID,
			"confirmation_required",
			fmt.Sprintf(
				"Session deletion was not confirmed. Rerun with --confirm to delete session %s.",
				sessionID,
			),
		), *jsonOut)
	}
	remote, err := c.dial()
	if err != nil {
		return writeSessionRemovalOutcome(
			stdout,
			stderr,
			nil,
			sessionDeleteFailure(sessionID, err),
			*jsonOut,
		)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result, err := runSessionDeleteUseCase(ctx, remote, sessionID)
	stop()
	var failure *sessionRemovalError
	if err != nil {
		failure = sessionDeleteFailure(sessionID, err)
	}
	exitCode = writeSessionRemovalOutcome(stdout, stderr, result, failure, *jsonOut)
	if closeErr := remote.Close(); closeErr != nil {
		fmt.Fprintf(stderr, "close Session deletion remote: %v\n", closeErr)
	}
	return exitCode
}

func (c sessionCommand) dial() (sessionCommandRemote, error) {
	if c.openRemote == nil {
		return nil, errors.New("Session command remote opener is required")
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), bindingCommandRPCTimeout)
	defer cancel()
	return c.openRemote(dialCtx)
}

func openSessionCommandRemote(ctx context.Context) (sessionCommandRemote, error) {
	_, remote, err := openBindingCommandRemote(ctx, ".")
	return remote, err
}

func runSessionArchiveUseCase(
	ctx context.Context,
	remote sessionCommandRemote,
	sessionID string,
	outputPath string,
) (*sessionRemovalResult, error) {
	selector, err := runtimeids.ParseSessionSelector(sessionID)
	if err != nil {
		return nil, err
	}
	caller, err := app.LiveSteerCallerSessionID()
	if err != nil {
		return nil, err
	}
	response, err := remote.ArchiveSession(ctx, &sessionlaunchpb.SessionArchiveRequest{
		SessionId:       protoapi.SessionSelectorToProto(selector),
		CallerSessionId: caller,
		OutputPath:      outputPath,
	})
	if err != nil {
		return nil, err
	}
	return &sessionRemovalResult{
		SessionID:  response.SessionId,
		OutputPath: &response.OutputPath,
	}, nil
}

func runSessionDeleteUseCase(
	ctx context.Context,
	remote sessionCommandRemote,
	sessionID string,
) (*sessionRemovalResult, error) {
	selector, err := runtimeids.ParseSessionSelector(sessionID)
	if err != nil {
		return nil, err
	}
	caller, err := app.LiveSteerCallerSessionID()
	if err != nil {
		return nil, err
	}
	response, err := remote.DeleteSession(ctx, &sessionlaunchpb.SessionDeleteRequest{
		SessionId:       protoapi.SessionSelectorToProto(selector),
		CallerSessionId: caller,
	})
	if err != nil {
		return nil, err
	}
	return &sessionRemovalResult{SessionID: response.SessionId}, nil
}

func sessionArchiveFailure(sessionID string, err error) *sessionRemovalError {
	var wire *client.SessionArchiveFailureError
	if !errors.As(err, &wire) || wire == nil || wire.Failure == nil {
		return sessionRemovalFailure(sessionID, "request_failed", err.Error())
	}
	failure := wire.Failure
	if self := failure.GetSelfTarget(); self != nil {
		return sessionRemovalFailure(self.SessionId, "self_session_forbidden", selfSessionRemovalMessage)
	}
	if expected := expectedSessionRemovalFailure(
		sessionID,
		failure.Code,
		failure.GetSessionNotFound(),
		failure.GetSessionInUse(),
	); expected != nil {
		return expected
	}
	switch failure.Code {
	case "invalid_output_path":
		if details := failure.GetInvalidOutputPath(); details != nil {
			return sessionRemovalFailure(
				sessionID,
				"invalid_output_path",
				invalidArchiveOutputPathMessage(details),
			)
		}
	case "output_exists":
		if details := failure.GetOutputExists(); details != nil {
			return sessionRemovalFailure(
				sessionID,
				"output_exists",
				fmt.Sprintf("Archive output already exists at %s.", details.Path),
			)
		}
	}
	return sessionRemovalFailure(sessionID, "request_failed", err.Error())
}

func sessionDeleteFailure(sessionID string, err error) *sessionRemovalError {
	var wire *client.SessionDeleteFailureError
	if !errors.As(err, &wire) || wire == nil || wire.Failure == nil {
		return sessionRemovalFailure(sessionID, "request_failed", err.Error())
	}
	failure := wire.Failure
	if self := failure.GetSelfTarget(); self != nil {
		return sessionRemovalFailure(self.SessionId, "self_session_forbidden", selfSessionRemovalMessage)
	}
	if expected := expectedSessionRemovalFailure(
		sessionID,
		failure.Code,
		failure.GetSessionNotFound(),
		failure.GetSessionInUse(),
	); expected != nil {
		return expected
	}
	return sessionRemovalFailure(sessionID, "request_failed", err.Error())
}

func expectedSessionRemovalFailure(
	sessionID string,
	code string,
	notFound *sessionlaunchpb.SessionNotFoundDetails,
	inUse *sessionlaunchpb.SessionInUseDetails,
) *sessionRemovalError {
	switch code {
	case "session_not_found":
		if notFound != nil {
			failure := sessionRemovalFailure(
				sessionID,
				"session_not_found",
				fmt.Sprintf("Session %s was not found; it might have been deleted.", sessionID),
			)
			failure.SessionID = notFound.SessionId
			return failure
		}
	case "session_in_use":
		if inUse != nil {
			identity, err := protoapi.SessionIdentityFromProto(inUse.SessionId, inUse.ShortId)
			if err != nil {
				return sessionRemovalFailure(inUse.SessionId, "request_failed", err.Error())
			}
			return sessionRemovalFailure(
				inUse.SessionId,
				"session_in_use",
				fmt.Sprintf("Session %s is in use and cannot be removed.", identity.String()),
			)
		}
	}
	return nil
}

func invalidArchiveOutputPathMessage(details *sessionlaunchpb.InvalidArchiveOutputPathDetails) string {
	switch details.Reason {
	case sessionlaunchpb.InvalidArchiveOutputPathReason_INVALID_ARCHIVE_OUTPUT_PATH_REASON_NOT_ABSOLUTE:
		return fmt.Sprintf("Archive output path %s must be absolute.", details.Path)
	case sessionlaunchpb.InvalidArchiveOutputPathReason_INVALID_ARCHIVE_OUTPUT_PATH_REASON_INVALID_SUFFIX:
		return fmt.Sprintf("Archive output path %s must end in .tar.zst.", details.Path)
	default:
		return fmt.Sprintf("Archive output path %s is invalid.", details.Path)
	}
}

func sessionRemovalFailure(
	sessionID string,
	code string,
	message string,
) *sessionRemovalError {
	var identity *string
	if selector, err := runtimeids.ParseSessionSelector(sessionID); err == nil {
		if id, ok := selector.(runtimeids.SessionID); ok {
			value := id.String()
			identity = &value
		}
	}
	return &sessionRemovalError{
		Code:      code,
		Message:   message,
		SessionID: identity,
	}
}

func writeSessionRemovalOutcome(
	stdout io.Writer,
	stderr io.Writer,
	result *sessionRemovalResult,
	failure *sessionRemovalError,
	jsonOut bool,
) int {
	if failure != nil {
		if jsonOut {
			if exitCode := writeCommandJSON(stdout, stderr, sessionRemovalJSONEnvelope{
				Status: "error",
				Error:  failure,
			}); exitCode != 0 {
				return exitCode
			}
			return 1
		}
		fmt.Fprintln(stderr, failure.Message)
		return 1
	}
	if jsonOut {
		return writeCommandJSON(stdout, stderr, sessionRemovalJSONEnvelope{
			Status: "ok",
			Result: result,
		})
	}
	if _, err := fmt.Fprintln(stdout, "done"); err != nil {
		fmt.Fprintf(stderr, "write Session removal result: %v\n", err)
		return 1
	}
	return 0
}
