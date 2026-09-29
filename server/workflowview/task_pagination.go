package workflowview

import (
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

func TaskPageWindow(request *taskpb.TaskOffsetPageRequest) serverapi.OffsetWindow {
	window := serverapi.OffsetWindow{Offset: int(request.GetOffset()), Limit: serverapi.OffsetPaginationMaxLimit}
	if request.Limit != nil {
		window.Limit = int(*request.Limit)
	}
	return window
}

func TaskNextOffset(offset *int) (*int32, error) {
	if offset == nil {
		return nil, nil
	}
	value, err := protoapi.Int32(*offset, "next_offset")
	if err != nil {
		return nil, err
	}
	return &value, nil
}
