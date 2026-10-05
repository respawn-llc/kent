package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata/sqlitegen"
	"core/server/session"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
)

func (s *Store) PrepareSessionExecutionTarget(ctx context.Context, update SessionExecutionTargetUpdate) (*worktreepb.SessionExecutionTarget, error) {
	if update.Workspace == nil || strings.TrimSpace(update.Workspace.ID) == "" {
		return nil, errors.New("prepared Session execution target requires a workspace")
	}
	workspace, err := s.queries.GetWorkspaceByID(ctx, update.Workspace.ID)
	if err != nil {
		return nil, err
	}
	row := sqlitegen.GetSessionExecutionTargetByIDRow{
		SessionID: update.SessionID, ProjectID: workspace.ProjectID,
		WorkspaceID: workspace.ID, WorkspaceRoot: workspace.CanonicalRootPath,
		CwdRelpath: update.CwdRelpath,
	}
	if update.Worktree != nil {
		worktree, err := sessionWorktreeForWorkspace(ctx, s.queries, update.Worktree.ID, workspace.ID)
		if err != nil {
			return nil, err
		}
		row.WorktreeID = sql.NullString{String: worktree.ID, Valid: true}
		row.WorktreeRoot = sql.NullString{String: worktree.CanonicalRootPath, Valid: true}
	}
	return sessionExecutionTargetFromRow(row)
}

// PreparedSessionSnapshot contains serialized Session facts, not a second
// persistence owner. Only the caller's cutover transaction publishes them.
type PreparedSessionSnapshot struct {
	id        runtimeids.SessionID
	params    sqlitegen.UpsertSessionParams
	workspace sqlitegen.Workspace
}

func (p PreparedSessionSnapshot) SessionID() runtimeids.SessionID { return p.id }

func (p PreparedSessionSnapshot) ExecutionTarget() SessionExecutionTargetUpdate {
	target := SessionExecutionTargetUpdate{
		SessionID:  p.id.String(),
		Workspace:  &SessionExecutionTargetUpdateWorkspace{ID: p.params.WorkspaceID.String},
		CwdRelpath: p.params.CwdRelpath,
	}
	if p.params.WorktreeID.Valid {
		target.Worktree = &SessionExecutionTargetUpdateWorktree{ID: p.params.WorktreeID.String}
	}
	return target
}

// PrepareSessionSnapshot performs path validation and serialization before the
// database-only cutover. Target ownership is checked again by the transaction.
func (s *Store) PrepareSessionSnapshot(ctx context.Context, snapshot session.PersistedStoreSnapshot, target SessionExecutionTargetUpdate) (PreparedSessionSnapshot, error) {
	id, err := runtimeids.ParseSessionID(snapshot.Meta.SessionID)
	if err != nil || !id.IsCanonicalUUIDv4() {
		return PreparedSessionSnapshot{}, errors.New("prepared Session requires a UUIDv4 identity")
	}
	if target.SessionID != id.String() {
		return PreparedSessionSnapshot{}, errors.New("prepared Session execution target identity does not match")
	}
	if target.Workspace == nil || strings.TrimSpace(target.Workspace.ID) == "" {
		return PreparedSessionSnapshot{}, errors.New("prepared Session requires a workspace")
	}
	if target.ExpectedWorktreeID != nil {
		return PreparedSessionSnapshot{}, errors.New("fresh Session has no previous worktree")
	}
	workspace, err := s.queries.GetWorkspaceByID(ctx, target.Workspace.ID)
	if err != nil {
		return PreparedSessionSnapshot{}, err
	}
	sameRoot, err := canonicalWorkspaceRootsEqual(snapshot.Meta.WorkspaceRoot, workspace.CanonicalRootPath)
	if err != nil {
		return PreparedSessionSnapshot{}, err
	}
	if !sameRoot {
		return PreparedSessionSnapshot{}, errors.New("prepared Session workspace does not match execution target")
	}
	snapshot.Meta.WorkspaceRoot = workspace.CanonicalRootPath
	if target.WorktreeReminder != nil {
		snapshot.Meta.WorktreeReminder = session.CloneWorktreeReminderState(target.WorktreeReminder)
	}
	params, err := s.serializeSessionSnapshot(snapshot)
	if err != nil {
		return PreparedSessionSnapshot{}, err
	}
	params.ProjectID = workspace.ProjectID
	params.WorkspaceID = sql.NullString{String: workspace.ID, Valid: true}
	params.CwdRelpath = normalizeSessionCwdRelpath(target.CwdRelpath)
	if target.Worktree != nil {
		if strings.TrimSpace(target.Worktree.ID) == "" {
			return PreparedSessionSnapshot{}, ErrWorktreeIDRequired
		}
		params.WorktreeID = sql.NullString{String: target.Worktree.ID, Valid: true}
	}
	prepared := PreparedSessionSnapshot{id: id, params: params, workspace: workspace}
	if err := prepared.validateTarget(ctx, s.queries); err != nil {
		return PreparedSessionSnapshot{}, err
	}
	return prepared, nil
}

func (p PreparedSessionSnapshot) validateTarget(ctx context.Context, q *sqlitegen.Queries) error {
	workspace, err := q.GetWorkspaceByID(ctx, p.params.WorkspaceID.String)
	if err != nil {
		return err
	}
	if workspace.ProjectID != p.workspace.ProjectID || workspace.CanonicalRootPath != p.workspace.CanonicalRootPath {
		return errors.New("prepared Session workspace changed before cutover")
	}
	if p.params.WorktreeID.Valid {
		_, err = sessionWorktreeForWorkspace(ctx, q, p.params.WorktreeID.String, workspace.ID)
		return err
	}
	return nil
}

func sessionWorktreeForWorkspace(ctx context.Context, q *sqlitegen.Queries, worktreeID, workspaceID string) (sqlitegen.GetWorktreeByIDRow, error) {
	if strings.TrimSpace(worktreeID) == "" {
		return sqlitegen.GetWorktreeByIDRow{}, ErrWorktreeIDRequired
	}
	worktree, err := q.GetWorktreeByID(ctx, worktreeID)
	if err != nil {
		return sqlitegen.GetWorktreeByIDRow{}, fmt.Errorf("get worktree by id: %w", err)
	}
	if worktree.WorkspaceID != workspaceID {
		return sqlitegen.GetWorktreeByIDRow{}, &WorktreeWorkspaceMismatchError{WorktreeID: worktreeID, WorkspaceID: workspaceID}
	}
	return worktree, nil
}

// InsertPreparedSession joins an existing transaction. It does not commit,
// access artifacts, invoke observers, or overwrite an existing Session.
func InsertPreparedSession(ctx context.Context, tx *sql.Tx, prepared PreparedSessionSnapshot) error {
	if tx == nil || prepared.id.IsZero() {
		return errors.New("Session cutover requires a transaction and prepared snapshot")
	}
	q := sqlitegen.New(tx)
	if _, err := q.AcquireWorkspaceRegistrationLock(ctx); err != nil {
		return err
	}
	if err := prepared.validateTarget(ctx, q); err != nil {
		return err
	}
	if _, err := q.GetSessionExecutionTargetByID(sqlitegen.WithExpectedNoRows(ctx), prepared.id.String()); err == nil {
		return errors.New("prepared Session identity already exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := q.UpsertSession(ctx, prepared.params); err != nil {
		return fmt.Errorf("insert prepared Session: %w", err)
	}
	return nil
}
