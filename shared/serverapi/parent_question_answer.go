package serverapi

import (
	"fmt"

	"core/shared/runtimeids"
)

type ParentQuestionAnswerRejectedError struct {
	AnsweringSessionID runtimeids.SessionID
	QuestionSessionID  runtimeids.SessionID
}

func (e *ParentQuestionAnswerRejectedError) Error() string {
	return fmt.Sprintf(
		"answering Session %s attempted to answer direct parent Session %s Question",
		e.AnsweringSessionID,
		e.QuestionSessionID,
	)
}
