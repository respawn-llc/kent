package protoapi

import (
	"time"

	"core/shared/transcript"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func CommittedTimeToProto(value *transcript.CommittedAtUnixMs) (*timestamppb.Timestamp, error) {
	if value == nil {
		return nil, nil
	}
	result := timestamppb.New(time.UnixMilli(value.UnixMs()))
	if err := result.CheckValid(); err != nil {
		return nil, err
	}
	return result, nil
}

func MillisecondsToProto(value int64) (*durationpb.Duration, error) {
	result := &durationpb.Duration{
		Seconds: value / 1000,
		Nanos:   int32(value%1000) * 1000000,
	}
	if err := result.CheckValid(); err != nil {
		return nil, err
	}
	return result, nil
}
