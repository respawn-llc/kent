package runtime

import (
	"fmt"

	"core/server/llm"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type AgentSteer struct {
	message       llm.Message
	initialPrompt *string
}

func NewAgentSteer(source runtimeids.SessionIdentity, text string) AgentSteer {
	content := fmt.Sprintf(
		"Agent from another session said:\n> %s\n\nYou can use `kent run steer %s \"message\"` to respond.",
		text,
		source.String(),
	)
	messageType := llm.MessageTypeAgentSteer
	return AgentSteer{initialPrompt: textutil.Value(text), message: llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: &messageType,
		Content:     textutil.Value(content),
	}}
}

func (s AgentSteer) Message() llm.Message {
	return s.message
}

func supervisorSteer(suggestions []string) AgentSteer {
	return AgentSteer{message: llm.Message{
		Role:        llm.RoleDeveloper,
		MessageType: textutil.Value(llm.MessageTypeReviewerFeedback),
		Content:     textutil.Value(formatReviewerDeveloperInstruction(suggestions)),
	}}
}
