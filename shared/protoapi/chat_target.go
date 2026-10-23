package protoapi

import (
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"google.golang.org/protobuf/proto"
)

type ChatTargetRequest interface {
	proto.Message
	GetTarget() *chatpb.ChatTarget
}
