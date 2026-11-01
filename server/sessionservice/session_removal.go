package sessionservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"core/server/sessionstore"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

const archiveTimeout = 5 * time.Minute

var ErrArchiveTimeout = errors.New("Session archive timeout exceeded")

type SessionRemovalCleanupError struct {
	RemainingPath string
	Cause         error
}

type SessionRemovalError struct {
	SessionID runtimeids.SessionID
	ShortID   *runtimeids.SessionShortID
	Cause     error
}

func (e *SessionRemovalError) Error() string { return e.Cause.Error() }
func (e *SessionRemovalError) Unwrap() error { return e.Cause }

func (s *SessionLifecycleService) removalTarget(ctx context.Context, target *sharedpb.SessionSelector, caller *string) (runtimeids.SessionID, error) {
	if s == nil || s.metadata == nil {
		return runtimeids.SessionID{}, errors.New("Session metadata is required")
	}
	id, err := s.metadata.ResolveSessionTarget(ctx, target)
	if err != nil {
		return runtimeids.SessionID{}, err
	}
	if caller != nil && *caller == id.String() {
		return runtimeids.SessionID{}, &serverapi.SessionSelfTargetError{SessionID: id}
	}
	return id, nil
}

func (e *SessionRemovalCleanupError) Error() string {
	return fmt.Sprintf(
		"Session is gone from Kent, but artifact cleanup failed. Remove %s manually: %v",
		e.RemainingPath,
		e.Cause,
	)
}

func (e *SessionRemovalCleanupError) Unwrap() error {
	return e.Cause
}

type ArchiveTimeoutDiagnostic struct {
	SessionID  string
	OutputPath string
	Cause      error
}

func logArchiveTimeout(diagnostic ArchiveTimeoutDiagnostic) {
	slog.Error(
		"accepted Session archive exceeded its timeout",
		"session_id", diagnostic.SessionID,
		"output_path", diagnostic.OutputPath,
		"error", diagnostic.Cause,
	)
}

func (s *SessionLifecycleService) WithDebugMode(debug bool) *SessionLifecycleService {
	if s != nil {
		s.debug = debug
	}
	return s
}

func (s *SessionLifecycleService) Archive(
	invocationCtx context.Context,
	sessionID string,
	outputPath string,
) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	if s.metadata == nil {
		return errors.New("Session removal metadata is required")
	}
	id := runtimeids.SessionIDFromProto(sessionID)
	if invocationCtx == nil {
		invocationCtx = context.Background()
	}
	if err := session.PreflightSessionArchiveDestination(outputPath); err != nil {
		return err
	}
	result, err := s.authority.AcceptLifecycleTask(func(lifecycleCtx context.Context) error {
		return s.runAcceptedArchive(lifecycleCtx, id, outputPath)
	})
	if err != nil {
		return err
	}
	return waitForAcceptedSessionOperation(invocationCtx, result)
}

func (s *SessionLifecycleService) Delete(invocationCtx context.Context, sessionID string) error {
	if s == nil || s.authority == nil {
		return errors.New("session runtime authority is required")
	}
	if s.metadata == nil {
		return errors.New("Session removal metadata is required")
	}
	id := runtimeids.SessionIDFromProto(sessionID)
	if invocationCtx == nil {
		invocationCtx = context.Background()
	}
	result, err := s.authority.AcceptLifecycleTask(func(lifecycleCtx context.Context) error {
		return s.authority.WithDestructiveSessionAdmission(
			lifecycleCtx,
			id,
			func(runCtx context.Context) error {
				return s.removeSessionUnderAdmission(runCtx, id)
			},
		)
	})
	if err != nil {
		return err
	}
	return waitForAcceptedSessionOperation(invocationCtx, result)
}

func (s *SessionLifecycleService) ArchiveSession(
	ctx context.Context,
	request *sessionlaunchpb.SessionArchiveRequest,
) (*sessionlaunchpb.SessionArchiveSuccess, error) {
	id, err := s.removalTarget(ctx, request.SessionId, request.CallerSessionId)
	if err != nil {
		return nil, err
	}
	identity, err := s.metadata.SessionIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Archive(ctx, id.String(), request.OutputPath); err != nil {
		return nil, &SessionRemovalError{SessionID: id, ShortID: identity.ShortID, Cause: err}
	}
	return &sessionlaunchpb.SessionArchiveSuccess{
		SessionId:  id.String(),
		OutputPath: request.OutputPath,
	}, nil
}

func (s *SessionLifecycleService) DeleteSession(
	ctx context.Context,
	request *sessionlaunchpb.SessionDeleteRequest,
) (*sessionlaunchpb.SessionDeleteSuccess, error) {
	id, err := s.removalTarget(ctx, request.SessionId, request.CallerSessionId)
	if err != nil {
		return nil, err
	}
	identity, err := s.metadata.SessionIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Delete(ctx, id.String()); err != nil {
		return nil, &SessionRemovalError{SessionID: id, ShortID: identity.ShortID, Cause: err}
	}
	return &sessionlaunchpb.SessionDeleteSuccess{SessionId: id.String()}, nil
}

func waitForAcceptedSessionOperation(invocationCtx context.Context, result <-chan error) error {
	select {
	case err := <-result:
		return err
	case <-invocationCtx.Done():
		return context.Cause(invocationCtx)
	}
}

func (s *SessionLifecycleService) runAcceptedArchive(
	lifecycleCtx context.Context,
	sessionID runtimeids.SessionID,
	outputPath string,
) error {
	operationCtx, cancelOperation := context.WithTimeout(lifecycleCtx, archiveTimeout)
	defer cancelOperation()
	operationDone := make(chan error, 1)
	go func() {
		operationDone <- s.authority.WithDestructiveSessionAdmission(
			operationCtx,
			sessionID,
			func(runCtx context.Context) error {
				record, err := session.ResolvePersistedSessionRecord(
					runCtx,
					s.metadata,
					sessionID.String(),
				)
				if err != nil {
					return err
				}
				if err := session.ArchiveSessionDirectory(
					runCtx,
					sessionID,
					record.SessionDir,
					outputPath,
				); err != nil {
					return err
				}
				if err := s.removeSessionUnderAdmission(runCtx, sessionID); err != nil {
					var cleanupErr *SessionRemovalCleanupError
					if errors.As(err, &cleanupErr) {
						return err
					}
					return fmt.Errorf(
						"archive %s exists, but Session %s was retained: %w. Resolve the blocker, then run kent session delete %s",
						outputPath,
						sessionID,
						err,
						sessionID,
					)
				}
				return nil
			},
		)
	}()

	select {
	case err := <-operationDone:
		if errors.Is(context.Cause(operationCtx), context.DeadlineExceeded) {
			return s.archiveTimeoutFailure(sessionID, outputPath, errors.Join(context.DeadlineExceeded, err))
		}
		return err
	case <-operationCtx.Done():
		cancelOperation()
		operationErr := <-operationDone
		cause := errors.Join(context.Cause(operationCtx), operationErr)
		if errors.Is(context.Cause(operationCtx), context.DeadlineExceeded) {
			return s.archiveTimeoutFailure(sessionID, outputPath, cause)
		}
		return cause
	}
}

func (s *SessionLifecycleService) archiveTimeoutFailure(sessionID runtimeids.SessionID, outputPath string, err error) error {
	cause := errors.Join(ErrArchiveTimeout, err)
	diagnostic := ArchiveTimeoutDiagnostic{
		SessionID:  sessionID.String(),
		OutputPath: outputPath,
		Cause:      cause,
	}
	logArchiveTimeout(diagnostic)
	if s.debug {
		panic(diagnostic)
	}
	return cause
}

func (s *SessionLifecycleService) removeSessionUnderAdmission(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) error {
	record, err := session.ResolvePersistedSessionRecord(ctx, s.metadata, sessionID.String())
	if err != nil {
		return err
	}
	schedule, err := session.PreflightSessionArtifactRemoval(record.SessionDir)
	if err != nil {
		return err
	}
	if err := s.metadata.DeleteSession(ctx, sessionID.String()); err != nil {
		return err
	}
	if err := session.RemovePreflightedSessionArtifacts(schedule); err != nil {
		var removalErr *session.SessionArtifactRemovalError
		if errors.As(err, &removalErr) {
			return &SessionRemovalCleanupError{
				RemainingPath: removalErr.RemainingPath,
				Cause:         err,
			}
		}
		return err
	}
	return nil
}
