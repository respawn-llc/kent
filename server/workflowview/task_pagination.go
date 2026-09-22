package workflowview

import "core/shared/protoapi"

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
