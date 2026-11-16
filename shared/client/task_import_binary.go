package client

import (
	"context"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

func (c *Remote) ImportTasks(ctx context.Context, request *taskimportpb.Request, progress serverapi.TaskImportProgressSink) (*taskimportpb.Success, error) {
	method := bootstrapMethod(taskimportpb.File_kent_api_task_import_task_import_proto, "TaskImportService", "Import")
	result := &taskimportpb.Result{}
	if err := c.callProgress(ctx, "task-import", method, request, result, func() proto.Message { return &taskimportpb.ProgressEvent{} }, func(event proto.Message) {
		if progress != nil {
			progress.PublishTaskImportProgress(event.(*taskimportpb.ProgressEvent))
		}
	}); err != nil {
		return nil, err
	}
	return decodeGeneratedResult(method, result, func(failure *taskimportpb.Error) error {
		if detail := failure.GetImportFailure(); detail != nil {
			return &serverapi.TaskImportError{Detail: detail}
		}
		return generatedOperationFailure(failure.Code)
	})
}
