package serverapi

import "core/shared/runtimeids"

const SessionPromptHistoryMaxEntries = 100

type SessionLaunchIntentKind string
type SessionCreateOriginKind string

const (
	SessionLaunchIntentCreateNew    SessionLaunchIntentKind = "create_new"
	SessionLaunchIntentOpenExisting SessionLaunchIntentKind = "open_existing"

	SessionCreateOriginIndependent     SessionCreateOriginKind = "independent"
	SessionCreateOriginPreviousSession SessionCreateOriginKind = "previous_session"
	SessionCreateOriginParentAgent     SessionCreateOriginKind = "parent_agent"
)

type SessionCreateOrigin struct {
	kind      SessionCreateOriginKind
	sessionID *runtimeids.SessionID
}

func IndependentSessionCreateOrigin() SessionCreateOrigin {
	return SessionCreateOrigin{kind: SessionCreateOriginIndependent}
}

func PreviousSessionCreateOrigin(sessionID runtimeids.SessionID) SessionCreateOrigin {
	return sessionCreateOriginWithID(SessionCreateOriginPreviousSession, sessionID)
}

func ParentAgentSessionCreateOrigin(sessionID runtimeids.SessionID) SessionCreateOrigin {
	return sessionCreateOriginWithID(SessionCreateOriginParentAgent, sessionID)
}

func sessionCreateOriginWithID(kind SessionCreateOriginKind, sessionID runtimeids.SessionID) SessionCreateOrigin {
	copied := sessionID
	return SessionCreateOrigin{kind: kind, sessionID: &copied}
}

func (o SessionCreateOrigin) Kind() SessionCreateOriginKind {
	return o.kind
}

func (o SessionCreateOrigin) SessionID() (runtimeids.SessionID, bool) {
	if o.sessionID == nil {
		return runtimeids.SessionID{}, false
	}
	return *o.sessionID, true
}

func (o SessionCreateOrigin) Equal(other SessionCreateOrigin) bool {
	if o.kind != other.kind {
		return false
	}
	left, leftPresent := o.SessionID()
	right, rightPresent := other.SessionID()
	return leftPresent == rightPresent && (!leftPresent || left == right)
}

type SessionLaunchIntent struct {
	kind      SessionLaunchIntentKind
	origin    *SessionCreateOrigin
	sessionID *runtimeids.SessionID
}

func CreateNewSessionLaunchIntent(origin SessionCreateOrigin) SessionLaunchIntent {
	copied := origin
	return SessionLaunchIntent{kind: SessionLaunchIntentCreateNew, origin: &copied}
}

func OpenExistingSessionLaunchIntent(sessionID runtimeids.SessionID) SessionLaunchIntent {
	copied := sessionID
	return SessionLaunchIntent{
		kind:      SessionLaunchIntentOpenExisting,
		sessionID: &copied,
	}
}

func (i SessionLaunchIntent) Kind() SessionLaunchIntentKind {
	return i.kind
}

func (i SessionLaunchIntent) CreateOrigin() (SessionCreateOrigin, bool) {
	if i.origin == nil {
		return SessionCreateOrigin{}, false
	}
	return *i.origin, true
}

func (i SessionLaunchIntent) SessionID() (runtimeids.SessionID, bool) {
	if i.sessionID == nil {
		return runtimeids.SessionID{}, false
	}
	return *i.sessionID, true
}

func (i SessionLaunchIntent) Equal(other SessionLaunchIntent) bool {
	if i.kind != other.kind {
		return false
	}
	leftOrigin, leftHasOrigin := i.CreateOrigin()
	rightOrigin, rightHasOrigin := other.CreateOrigin()
	if leftHasOrigin != rightHasOrigin || (leftHasOrigin && !leftOrigin.Equal(rightOrigin)) {
		return false
	}
	leftSession, leftHasSession := i.SessionID()
	rightSession, rightHasSession := other.SessionID()
	return leftHasSession == rightHasSession && (!leftHasSession || leftSession == rightSession)
}
