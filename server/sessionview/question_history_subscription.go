package sessionview

import (
	"context"
	"io"

	"core/server/session"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/serverapi"
)

const largeQuestionHistoryBytes = int64(1_073_741_824)

type questionHistorySubscription struct {
	cursor    *session.QuestionHistoryCursor
	started   bool
	completed bool
}

func (s *questionHistorySubscription) Next(ctx context.Context) (*sessionpb.QuestionHistoryEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.started {
		s.started = true
		large := s.cursor.InitialSize() >= largeQuestionHistoryBytes
		event := &sessionpb.QuestionHistoryEvent{
			Event: &sessionpb.QuestionHistoryEvent_Started{Started: &sessionpb.QuestionHistoryStarted{LargeHistory: large}},
		}
		return event, protoapi.Validate(event)
	}
	for !s.completed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := s.cursor.Next(ctx)
		if err != nil {
			return nil, err
		}
		if record == nil {
			s.completed = true
			omitted := s.cursor.HistoryOmitted()
			event := &sessionpb.QuestionHistoryEvent{
				Event: &sessionpb.QuestionHistoryEvent_Completed{Completed: &sessionpb.QuestionHistoryCompleted{HistoryOmitted: omitted}},
			}
			return event, protoapi.Validate(event)
		}
		question, err := projectQuestionHistoryRecord(*record, s.cursor.Version())
		if err != nil {
			return nil, err
		}
		if question == nil {
			continue
		}
		at, err := protoapi.CommittedTimeToProto(question.At)
		if err != nil {
			return nil, err
		}
		var selected *int32
		if question.SelectedOptionNumber != nil {
			value, err := protoapi.Int32(*question.SelectedOptionNumber, "selected option number")
			if err != nil {
				return nil, err
			}
			selected = &value
		}
		event := &sessionpb.QuestionHistoryEvent{
			Event: &sessionpb.QuestionHistoryEvent_Question{Question: &sessionpb.QuestionHistoryQuestion{
				Question:             question.Question,
				Answer:               question.Answer,
				SelectedOptionNumber: selected,
				Commentary:           question.Commentary,
				CommittedAt:          at,
			}},
		}
		return event, protoapi.Validate(event)
	}
	return nil, io.EOF
}

func (s *questionHistorySubscription) Close() error {
	if s == nil || s.cursor == nil {
		return nil
	}
	err := s.cursor.Close()
	s.cursor = nil
	return err
}

var _ serverapi.QuestionHistorySubscription = (*questionHistorySubscription)(nil)
