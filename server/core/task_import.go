package core

import (
	"context"
	"core/server/taskimport"
	"core/server/workflowexecution"
	"core/shared/apicontract"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
)

type ownedTaskImport struct {
	importer *taskimport.Service
	owner    *workflowexecution.CurrentNodeController
}

func (s ownedTaskImport) ImportTasks(ctx context.Context, request *taskimportpb.Request, progress serverapi.TaskImportProgressSink) (*taskimportpb.Success, error) {
	var result *taskimportpb.Success
	err := s.owner.RunTaskOperation(ctx, func(owned context.Context) error {
		var err error
		result, err = s.importer.ImportTasks(owned, request, progress)
		return err
	})
	return result, err
}

func (s *Core) TaskImportClient() apicontract.TaskImportService {
	return s.safeBundles().Workflows.imports
}
