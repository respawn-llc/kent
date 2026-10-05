package protoapi

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"google.golang.org/protobuf/proto"
)

func TestThinkingNoticeValidation(t *testing.T) {
	valid := &transcriptpb.NoticeRow{
		Reason:         transcriptpb.NoticeReason_NOTICE_REASON_THINKING_UPDATE,
		Severity:       transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
		ThinkingEffort: proto.String("high"),
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("validate Thinking notice: %v", err)
	}
	providerLevel := proto.Clone(valid).(*transcriptpb.NoticeRow)
	providerLevel.ThinkingEffort = proto.String("provider-effort")
	if err := Validate(providerLevel); err != nil {
		t.Fatalf("validate provider Thinking effort: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*transcriptpb.NoticeRow)
	}{
		{"missing effort", func(row *transcriptpb.NoticeRow) { row.ThinkingEffort = nil }},
		{"empty effort", func(row *transcriptpb.NoticeRow) { row.ThinkingEffort = proto.String("") }},
		{"blank effort", func(row *transcriptpb.NoticeRow) { row.ThinkingEffort = proto.String(" \t\n") }},
		{"warning", func(row *transcriptpb.NoticeRow) { row.Severity = transcriptpb.NoticeSeverity_NOTICE_SEVERITY_WARNING }},
		{"message type", func(row *transcriptpb.NoticeRow) {
			row.MessageType = transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENT_STEER.Enum()
		}},
		{"legacy text", func(row *transcriptpb.NoticeRow) { row.LegacyText = proto.String("notice") }},
		{"cache warning", func(row *transcriptpb.NoticeRow) {
			row.CacheWarning = &transcriptpb.CacheWarning{
				Scope: "turn", Reason: "miss", Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
			}
		}},
		{"compaction", func(row *transcriptpb.NoticeRow) { row.Compaction = &transcriptpb.CompactionNotice{} }},
		{"tool repair", func(row *transcriptpb.NoticeRow) {
			row.ToolOutputRepair = &transcriptpb.ToolOutputRepair{Kind: "fresh_resource", Count: 1}
		}},
		{"model mismatch", func(row *transcriptpb.NoticeRow) {
			row.ProviderModelMismatch = &transcriptpb.ProviderModelMismatch{RequestedModel: "requested", ServedModel: "served"}
		}},
		{"diagnostic", func(row *transcriptpb.NoticeRow) {
			row.Diagnostic = &transcriptpb.Diagnostic{Code: "failure", Detail: "details"}
		}},
		{"background", func(row *transcriptpb.NoticeRow) {
			row.Background = &transcriptpb.BackgroundNoticeIdentity{
				ActivityId: "58e121b5-30f7-4d0f-a1fa-fb3e6695e39c", ProcessId: "process",
			}
		}},
		{"other reason with effort", func(row *transcriptpb.NoticeRow) {
			row.Reason = transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE
			row.LegacyText = proto.String("notice")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := proto.Clone(valid).(*transcriptpb.NoticeRow)
			test.mutate(row)
			if err := Validate(row); err == nil {
				t.Fatal("invalid notice accepted")
			}
		})
	}
}
