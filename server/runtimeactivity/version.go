package runtimeactivity

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

var (
	processEpoch     = "process-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	fallbackSequence atomic.Uint64
)

func NextReadModelVersion(sessionID string) *runtimepb.ReadModelVersion {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = "unknown"
	}
	version, err := protoapi.NewReadModelVersion(
		processEpoch+"-"+id,
		1,
		fallbackSequence.Add(1),
	)
	if err != nil {
		panic(err)
	}
	return version
}

func BuildFeedSnapshot(
	version *runtimepb.ReadModelVersion,
	resolver ResolverSnapshot,
) (*runtimepb.ReadModelUpdate, error) {
	activity, err := resolveRuntimeFeedActivity(resolver)
	if err != nil {
		return &runtimepb.ReadModelUpdate{}, err
	}
	update := &runtimepb.ReadModelUpdate{Version: version, Activity: activity}
	if err := protoapi.Validate(update); err != nil {
		return &runtimepb.ReadModelUpdate{}, fmt.Errorf("validate runtime feed read-model update: %w", err)
	}
	return update, nil
}
