package serverapi

import taskimportpb "core/shared/protoapi/gen/kent/api/task_import"

type TaskImportError struct{ Detail *taskimportpb.Failure }

func (e *TaskImportError) Error() string { return e.Detail.Diagnostic }

type TaskImportProgressSink interface {
	PublishTaskImportProgress(*taskimportpb.ProgressEvent)
}

type TaskImportProgressFunc func(*taskimportpb.ProgressEvent)

func (fn TaskImportProgressFunc) PublishTaskImportProgress(event *taskimportpb.ProgressEvent) {
	if fn != nil {
		fn(event)
	}
}
