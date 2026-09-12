package chatcontext

import (
	"testing"

	"core/shared/protoapi"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	"google.golang.org/protobuf/proto"
)

func TestProjectNormalizesAndDerivesContextFacts(t *testing.T) {
	tests := []struct {
		name  string
		input ProjectionInput
		want  *contextpb.Context
	}{
		{
			name: "lazy zero usage",
			input: ProjectionInput{
				Policy:                Policy{ContextWindowTokens: 100, AutomaticThresholdTokens: 80, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL},
				AutoCompactionEnabled: true,
			},
			want: &contextpb.Context{
				ContextWindowTokens: 100, RemainingTokens: 100, AutomaticThresholdTokens: 80,
				AutoCompactionEnabled: true, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
			},
		},
		{
			name: "usage above window remains overrun",
			input: ProjectionInput{
				Policy:     Policy{ContextWindowTokens: 100, AutomaticThresholdTokens: 80, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE},
				UsedTokens: 125,
			},
			want: &contextpb.Context{
				ContextWindowTokens: 100, UsedTokens: 125, RemainingTokens: -25, AutomaticThresholdTokens: 80,
				CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE,
			},
		},
		{
			name: "invalid numeric facts normalize independently",
			input: ProjectionInput{
				Policy: Policy{
					ContextWindowTokens:      200,
					AutomaticThresholdTokens: 300,
					CompactionMode:           contextpb.CompactionMode_COMPACTION_MODE_DISABLED,
				},
				UsedTokens:               -7,
				CompletedCompactionCount: -3,
				ManualCompactEligible:    true,
			},
			want: &contextpb.Context{
				ContextWindowTokens: 200, RemainingTokens: 200, AutomaticThresholdTokens: 200,
				CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_DISABLED,
			},
		},
		{
			name: "negative threshold clamps to zero",
			input: ProjectionInput{
				Policy: Policy{ContextWindowTokens: 100, AutomaticThresholdTokens: -4, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL},
			},
			want: &contextpb.Context{
				ContextWindowTokens: 100, RemainingTokens: 100, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
			},
		},
		{
			name: "running compaction preserves usage and prevents manual Compact",
			input: ProjectionInput{
				Policy:                   Policy{ContextWindowTokens: 100, AutomaticThresholdTokens: 80, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL},
				UsedTokens:               45,
				CompletedCompactionCount: 2,
				CompactionRunning:        true,
				ManualCompactEligible:    true,
			},
			want: &contextpb.Context{
				ContextWindowTokens: 100, UsedTokens: 45, RemainingTokens: 55, AutomaticThresholdTokens: 80,
				CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_LOCAL, CompletedCompactionCount: 2, CompactionRunning: true,
			},
		},
		{
			name: "eligible idle enabled mode permits manual Compact",
			input: ProjectionInput{
				Policy:                Policy{ContextWindowTokens: 100, AutomaticThresholdTokens: 80, CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE},
				ManualCompactEligible: true,
			},
			want: &contextpb.Context{
				ContextWindowTokens: 100, RemainingTokens: 100, AutomaticThresholdTokens: 80,
				CompactionMode: contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE, ManualCompactAvailable: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Project(test.input)
			if !proto.Equal(got, test.want) {
				t.Fatalf("Project() = %+v, want %+v", got, test.want)
			}
			if err := protoapi.Validate(got); err != nil {
				t.Fatalf("projected Context is invalid: %v", err)
			}
		})
	}
}
