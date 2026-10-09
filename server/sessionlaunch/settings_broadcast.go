package sessionlaunch

import (
	"context"
	"errors"
	"io"
	"sync"

	"core/shared/apicontract"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

const settingsSubscriptionBufferSize = 64

// SettingsBroadcaster delivers committed Session projections independently of
// Agent runtime and client lifetimes. It retains only bounded delivery queues.
type SettingsBroadcaster struct {
	mu          sync.Mutex
	closed      bool
	subscribers map[runtimeids.SessionID]map[*settingsSubscription]struct{}
}

type settingsSubscription struct {
	owner     *SettingsBroadcaster
	sessionID runtimeids.SessionID
	events    chan *chatsettingspb.SettingsSnapshot
	err       error
}

func NewSettingsBroadcaster() *SettingsBroadcaster {
	return &SettingsBroadcaster{subscribers: make(map[runtimeids.SessionID]map[*settingsSubscription]struct{})}
}

func (b *SettingsBroadcaster) Subscribe(sessionID runtimeids.SessionID) (apicontract.ChatSettingsSubscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, serverapi.ErrStreamUnavailable
	}
	sub := &settingsSubscription{owner: b, sessionID: sessionID, events: make(chan *chatsettingspb.SettingsSnapshot, settingsSubscriptionBufferSize)}
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = make(map[*settingsSubscription]struct{})
	}
	b.subscribers[sessionID][sub] = struct{}{}
	return sub, nil
}

func (b *SettingsBroadcaster) Publish(snapshot *chatsettingspb.SettingsSnapshot) error {
	if err := protoapi.Validate(snapshot); err != nil {
		return err
	}
	sessionID, err := runtimeids.ParseSessionID(snapshot.Session.SessionId)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return serverapi.ErrStreamUnavailable
	}
	for sub := range b.subscribers[sessionID] {
		select {
		case sub.events <- proto.Clone(snapshot).(*chatsettingspb.SettingsSnapshot):
		default:
			b.closeLocked(sub, serverapi.ErrStreamGap)
		}
	}
	return nil
}

func (b *SettingsBroadcaster) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, subscribers := range b.subscribers {
		for sub := range subscribers {
			b.closeLocked(sub, io.EOF)
		}
	}
	return nil
}

func (b *SettingsBroadcaster) closeLocked(sub *settingsSubscription, err error) {
	if _, exists := b.subscribers[sub.sessionID][sub]; !exists {
		return
	}
	delete(b.subscribers[sub.sessionID], sub)
	if len(b.subscribers[sub.sessionID]) == 0 {
		delete(b.subscribers, sub.sessionID)
	}
	sub.err = err
	close(sub.events)
}

func (s *settingsSubscription) Next(ctx context.Context) (*chatsettingspb.SettingsSnapshot, error) {
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case event, ok := <-s.events:
		if ok {
			return event, nil
		}
		s.owner.mu.Lock()
		defer s.owner.mu.Unlock()
		return nil, s.err
	}
}

func (s *settingsSubscription) Close() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.owner.closeLocked(s, io.EOF)
	return nil
}

func (s *Service) SubscribeChatSettings(ctx context.Context, sessionID runtimeids.SessionID) (apicontract.ChatSettingsSubscription, error) {
	if s.settingsOwner.Changes == nil {
		return nil, errors.New("Session settings broadcaster is required")
	}
	if _, err := s.planner.PersistedSessions.ResolvePersistedSession(ctx, sessionID.String()); err != nil {
		return nil, err
	}
	return s.settingsOwner.Changes.Subscribe(sessionID)
}

func (s *Service) PublishSessionSettings(ctx context.Context, sessionID runtimeids.SessionID) error {
	if s.settingsOwner.Changes == nil {
		return errors.New("Session settings broadcaster is required")
	}
	projected, err := s.SessionChatSettings(ctx, sessionID)
	if err != nil {
		return err
	}
	record, err := s.planner.PersistedSessions.ResolvePersistedSession(ctx, sessionID.String())
	if err != nil {
		return err
	}
	settings := projected.GetSession()
	return s.settingsOwner.Changes.Publish(&chatsettingspb.SettingsSnapshot{
		SessionName: record.Meta.Name,
		Settings:    settings.Settings,
		Session:     settings.Session,
	})
}
