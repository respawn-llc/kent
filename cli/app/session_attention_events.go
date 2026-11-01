package app

import (
	"context"
	"errors"
	"io"

	"core/shared/apicontract"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
)

type sessionAttentionEvent struct {
	turnEnd *attentionpb.SessionTurnEnd
	failure *attentionpb.SessionNamingFailure
	err     error
}

func startSessionAttentionEvents(parent context.Context, id string, client apicontract.AttentionNotificationService) (<-chan sessionAttentionEvent, func()) {
	ctx, cancel := context.WithCancel(parent)
	events := make(chan sessionAttentionEvent, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(events)
		publish := func(event sessionAttentionEvent) {
			select {
			case events <- event:
			case <-ctx.Done():
			}
		}
		subscription, err := client.SubscribeSessionAttentionNotifications(ctx, &attentionpb.SubscribeRequest{SessionId: id})
		if err != nil {
			if ctx.Err() == nil {
				publish(sessionAttentionEvent{err: err})
			}
			return
		}
		defer subscription.Close()
		for {
			event, err := subscription.Next(ctx)
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, io.EOF) {
					publish(sessionAttentionEvent{err: err})
				}
				return
			}
			if failure := event.GetSessionNamingFailure(); failure != nil {
				publish(sessionAttentionEvent{failure: failure})
			}
			if turnEnd := event.GetSessionTurnEnd(); turnEnd != nil {
				publish(sessionAttentionEvent{turnEnd: turnEnd})
			}
		}
	}()
	return events, func() { cancel(); <-done }
}
