package serverapi

import "errors"

var ErrWorkflowTaskNotFound = errors.New("workflow task not found")

var ErrWorkflowNotFound = errors.New("workflow not found")
