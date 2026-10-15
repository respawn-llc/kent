package session

import (
	"reflect"
	"testing"
)

func TestGenerationContextRoundTrip(t *testing.T) {
	for _, version := range []int{EventLogVersionV1, EventLogVersionV2} {
		t.Run(eventLogVersionTestName(version), func(t *testing.T) {
			record, err := NewEventRecord(4, nil, GenerationContextRecord{
				BeforeSummary: []MessageRecord{{
					Role: MessageRoleDeveloper, MessageType: messageTypePointer(MessageTypeAgentsMD),
					Content: stringPointer("instructions"),
				}},
				AfterSummary: []MessageRecord{{
					Role: MessageRoleDeveloper, MessageType: messageTypePointer(MessageTypeEnvironment),
					Content: stringPointer("environment"),
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encodeEventRecordForVersion(version, record)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeEventRecordForVersion(version, encoded)
			if err != nil {
				t.Fatal(err)
			}
			want, err := record.Payload()
			if err != nil {
				t.Fatal(err)
			}
			got, err := decoded.Payload()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("operation context changed on reopen: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestGenerationContextRejectsInvalidMessages(t *testing.T) {
	for _, record := range []GenerationContextRecord{
		{},
		{BeforeSummary: []MessageRecord{{Role: MessageRoleUser, Content: stringPointer("user")}}},
		{AfterSummary: []MessageRecord{{Role: MessageRoleTool, Content: stringPointer("result")}}},
		{AfterSummary: []MessageRecord{{Role: MessageRoleDeveloper, Content: stringPointer("")}}},
	} {
		if _, err := NewEventRecord(1, nil, record); err == nil {
			t.Fatalf("invalid operation context accepted: %+v", record)
		}
	}
}
