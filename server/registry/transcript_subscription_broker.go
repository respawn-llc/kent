package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"core/shared/clientui"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"
	"core/shared/textutil"
)

const transcriptSubscriptionBufferSize = 256

type transcriptSubscriptionBroker struct {
	mu          sync.Mutex
	nextID      uint64
	closed      bool
	subscribers map[uint64]*transcriptSubscription
}

type transcriptSubscription struct {
	ch      chan *transcriptpb.Message
	onClose func()

	mu       sync.Mutex
	nextSeq  uint64
	err      error
	done     bool
	contract transcriptSubscriptionContract
}

var transcriptContractViolationsPanic bool

func newTranscriptSubscriptionBroker() *transcriptSubscriptionBroker {
	return &transcriptSubscriptionBroker{subscribers: make(map[uint64]*transcriptSubscription)}
}

func (b *transcriptSubscriptionBroker) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}

func (b *transcriptSubscriptionBroker) Subscribe(hydration *transcriptpb.Event) (*transcriptSubscription, error) {
	if b == nil {
		return nil, fmt.Errorf("transcript stream is unavailable: %w", serverapi.ErrStreamUnavailable)
	}
	sub := &transcriptSubscription{ch: make(chan *transcriptpb.Message, transcriptSubscriptionBufferSize)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		sub.closeWithError(io.EOF)
		return sub, nil
	}
	id := b.nextID
	b.nextID++
	if err := sub.publish(hydration); err != nil {
		b.mu.Unlock()
		sub.closeWithError(transcriptPublishError(err))
		return sub, nil
	}
	sub.onClose = func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
	b.subscribers[id] = sub
	b.mu.Unlock()
	return sub, nil
}

func (b *transcriptSubscriptionBroker) Publish(events []*transcriptpb.Event) {
	if b == nil || len(events) == 0 {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	subs := make([]*transcriptSubscription, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		for _, event := range events {
			if err := sub.publish(event); err != nil {
				sub.closeWithError(transcriptPublishError(err))
				break
			}
		}
	}
}

func (b *transcriptSubscriptionBroker) Close(err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.detachSubscribersLocked()
	b.mu.Unlock()
	closeTranscriptSubscribers(subs, err)
}

func (b *transcriptSubscriptionBroker) CloseSubscribers(err error) {
	if b == nil {
		return
	}
	subs := b.detachSubscribers()
	closeTranscriptSubscribers(subs, err)
}

func (b *transcriptSubscriptionBroker) detachSubscribers() []*transcriptSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.detachSubscribersLocked()
}

func (b *transcriptSubscriptionBroker) detachSubscribersLocked() []*transcriptSubscription {
	subs := make([]*transcriptSubscription, 0, len(b.subscribers))
	for id, sub := range b.subscribers {
		subs = append(subs, sub)
		delete(b.subscribers, id)
	}
	return subs
}

func closeTranscriptSubscribers(subs []*transcriptSubscription, err error) {
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

func (s *transcriptSubscription) publish(event *transcriptpb.Event) error {
	if s == nil {
		return errTranscriptContractViolation("transcript subscription is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return io.EOF
	}
	message := &transcriptpb.Message{Sequence: s.nextSeq + 1, Event: event}
	if err := s.contract.Validate(message); err != nil {
		return err
	}
	select {
	case s.ch <- message:
		s.nextSeq = message.Sequence
		return nil
	default:
		return serverapi.NewTranscriptStreamError(serverapi.TranscriptCloseReasonSubscriberOverflow, serverapi.ErrStreamGap)
	}
}

type transcriptContractViolation struct {
	message string
}

func errTranscriptContractViolation(message string) transcriptContractViolation {
	return transcriptContractViolation{message: strings.TrimSpace(message)}
}

func (e transcriptContractViolation) Error() string {
	if e.message == "" {
		return "transcript contract violation"
	}
	return "transcript contract violation: " + e.message
}

func transcriptPublishError(err error) error {
	if err == nil {
		return nil
	}
	var streamErr serverapi.TranscriptStreamError
	if errors.As(err, &streamErr) {
		return err
	}
	var violation transcriptContractViolation
	if errors.As(err, &violation) {
		if transcriptContractViolationsPanic {
			panic(violation)
		}
		log.Printf("transcript contract violation: %v", violation)
	}
	return serverapi.NewTranscriptStreamError(serverapi.TranscriptCloseReasonContractViolation, err)
}

func transcriptProjectionContractError(err error) error {
	if err == nil {
		return nil
	}
	return transcriptPublishError(errTranscriptContractViolation(err.Error()))
}

type transcriptSubscriptionContract struct {
	hydrated                 bool
	activeStream             *string
	inFlightTools            map[clientui.ToolCallID]struct{}
	hydratedEventSequence    int64
	hasHydratedEventSequence bool
	liveLocator              *transcriptpb.CommittedRowLocator
}

func (c *transcriptSubscriptionContract) Validate(message *transcriptpb.Message) error {
	if err := protoapi.Validate(message); err != nil {
		return errTranscriptContractViolation(err.Error())
	}
	if !c.hydrated {
		hydration := message.Event.GetHydration()
		if hydration == nil {
			return errTranscriptContractViolation(fmt.Sprintf("first message must be hydration seq=1, got payload=%T seq=%d", message.Event.Payload, message.Sequence))
		}
		c.hydrated = true
		return c.validateHydration(hydration)
	}
	if message.Event.GetHydration() != nil {
		return errTranscriptContractViolation(fmt.Sprintf("hydration repeated at seq=%d", message.Sequence))
	}
	return c.validateLiveMessage(message)
}

func (c *transcriptSubscriptionContract) validateHydration(hydration *transcriptpb.Hydration) error {
	if hydration.ActiveAssistant != nil {
		c.activeStream = textutil.Value(hydration.ActiveAssistant.StreamId)
	}
	for _, tool := range hydration.InFlightTools {
		if err := c.trackToolStart(tool, "hydration in-flight tool"); err != nil {
			return err
		}
	}
	for _, row := range hydration.TailSegment.Entries {
		if err := validateCommittedRow(row); err != nil {
			return err
		}
		if !c.hasHydratedEventSequence || row.Locator.EventSequence > c.hydratedEventSequence {
			c.hydratedEventSequence = row.Locator.EventSequence
			c.hasHydratedEventSequence = true
		}
	}
	return nil
}

func (c *transcriptSubscriptionContract) validateLiveMessage(message *transcriptpb.Message) error {
	switch payload := message.Event.Payload.(type) {
	case *transcriptpb.Event_AssistantDelta:
		return c.matchOrStartStream(payload.AssistantDelta.StreamId, message.Sequence, "assistant_delta")
	case *transcriptpb.Event_AssistantStreamAbort:
		if err := c.matchActiveStream(payload.AssistantStreamAbort.StreamId, message.Sequence, "assistant_stream_abort"); err != nil {
			return err
		}
		c.activeStream = nil
		return nil
	case *transcriptpb.Event_ToolStart:
		return c.trackToolStart(payload.ToolStart, fmt.Sprintf("tool_start at seq=%d", message.Sequence))
	case *transcriptpb.Event_ToolAbort:
		return c.trackToolTerminal(clientui.ToolCallID(payload.ToolAbort.ToolCallId), fmt.Sprintf("tool_abort at seq=%d", message.Sequence))
	case *transcriptpb.Event_CommittedRow:
		row := payload.CommittedRow
		if err := validateCommittedRow(row); err != nil {
			return err
		}
		if err := c.validateLiveLocator(row.Locator); err != nil {
			return err
		}
		if assistant := row.GetAssistant(); assistant != nil {
			if assistant.StreamId != nil {
				if err := c.matchActiveStream(*assistant.StreamId, message.Sequence, "committed assistant row"); err != nil {
					return err
				}
				c.activeStream = nil
			} else if c.activeStream != nil {
				return errTranscriptContractViolation(fmt.Sprintf("committed assistant row at seq=%d has nil stream_id while stream %s is active", message.Sequence, *c.activeStream))
			}
		}
		if tool := row.GetTool(); tool != nil && tool.ToolCallId != nil {
			return c.trackToolTerminal(clientui.ToolCallID(*tool.ToolCallId), fmt.Sprintf("committed tool row at seq=%d", message.Sequence))
		}
	}
	return nil
}

func (c *transcriptSubscriptionContract) validateLiveLocator(locator *transcriptpb.CommittedRowLocator) error {
	if c.liveLocator == nil {
		if c.hasHydratedEventSequence && locator.EventSequence <= c.hydratedEventSequence {
			return errTranscriptContractViolation(fmt.Sprintf("first live committed row event sequence %d is not newer than hydrated sequence %d", locator.EventSequence, c.hydratedEventSequence))
		}
		if locator.RowOrdinal != 1 {
			return errTranscriptContractViolation(fmt.Sprintf("first live committed row for event %d has ordinal %d, want 1", locator.EventSequence, locator.RowOrdinal))
		}
		c.liveLocator = locator
		return nil
	}
	if locator.EventSequence < c.liveLocator.EventSequence {
		return errTranscriptContractViolation(fmt.Sprintf("live committed row event sequence regressed from %d to %d", c.liveLocator.EventSequence, locator.EventSequence))
	}
	if locator.EventSequence == c.liveLocator.EventSequence {
		if locator.RowOrdinal != c.liveLocator.RowOrdinal+1 {
			return errTranscriptContractViolation(fmt.Sprintf("live committed row ordinal for event %d = %d, want %d", locator.EventSequence, locator.RowOrdinal, c.liveLocator.RowOrdinal+1))
		}
	} else if locator.RowOrdinal != 1 {
		return errTranscriptContractViolation(fmt.Sprintf("live committed row for event %d starts at ordinal %d, want 1", locator.EventSequence, locator.RowOrdinal))
	}
	c.liveLocator = locator
	return nil
}

func (c *transcriptSubscriptionContract) matchOrStartStream(streamID string, seq uint64, op string) error {
	if c.activeStream == nil {
		c.activeStream = &streamID
		return nil
	}
	return c.matchActiveStream(streamID, seq, op)
}

func (c *transcriptSubscriptionContract) matchActiveStream(streamID string, seq uint64, op string) error {
	if c.activeStream == nil {
		return errTranscriptContractViolation(fmt.Sprintf("%s at seq=%d has stream_id %s with no active assistant stream", op, seq, streamID))
	}
	if *c.activeStream != streamID {
		return errTranscriptContractViolation(fmt.Sprintf("%s at seq=%d has stream_id %s, active stream_id is %s", op, seq, streamID, *c.activeStream))
	}
	return nil
}

func (c *transcriptSubscriptionContract) trackToolStart(tool *transcriptpb.ToolStart, op string) error {
	toolID := clientui.ToolCallID(strings.TrimSpace(tool.ToolCallId))
	if toolID == "" {
		return errTranscriptContractViolation(op + " has empty tool_call_id")
	}
	if c.inFlightTools == nil {
		c.inFlightTools = make(map[clientui.ToolCallID]struct{})
	}
	if _, exists := c.inFlightTools[toolID]; exists {
		return nil
	}
	c.inFlightTools[toolID] = struct{}{}
	return nil
}

func (c *transcriptSubscriptionContract) trackToolTerminal(toolCallID clientui.ToolCallID, op string) error {
	toolID := clientui.ToolCallID(strings.TrimSpace(string(toolCallID)))
	if toolID == "" {
		return errTranscriptContractViolation(op + " has empty tool_call_id")
	}
	delete(c.inFlightTools, toolID)
	return nil
}

func validateCommittedRow(row *transcriptpb.CommittedRow) error {
	if err := protoapi.Validate(row); err != nil {
		return errTranscriptContractViolation(err.Error())
	}
	return nil
}

func (s *transcriptSubscription) Next(ctx context.Context) (*transcriptpb.Message, error) {
	if s == nil {
		return nil, io.EOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case evt, ok := <-s.ch:
		if ok {
			return evt, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return nil, serverapi.NormalizeStreamError(s.err)
		}
		return nil, io.EOF
	}
}

func (s *transcriptSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *transcriptSubscription) closeWithError(err error) {
	if s == nil {
		return
	}
	var onClose func()
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	close(s.ch)
	onClose = s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
}

var _ serverapi.TranscriptSubscription = (*transcriptSubscription)(nil)
