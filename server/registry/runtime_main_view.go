package registry

import (
	"core/server/runtimeview"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
)

func (r *RuntimeRegistry) RuntimeMainViewSnapshot(sessionID string) (*runtimepb.MainView, bool) {
	entry := r.authorityEntryBySession(sessionID)
	if entry == nil {
		return nil, false
	}
	view := entry.mainView.Load()
	if view == nil {
		return nil, false
	}
	return cloneRuntimeMainView(view), true
}

func (r *RuntimeRegistry) publishTranscriptAndMainView(entry *authorityRuntimeEntry, build func() ([]*transcriptpb.Event, error)) error {
	entry.publicationMu.Lock()
	defer entry.publicationMu.Unlock()
	if err := entry.sessionFeed.PublishBuilt(build); err != nil {
		return err
	}
	view := entry.mainView.Load()
	if view == nil {
		return nil
	}
	return r.publishRuntimeMainViewLocked(entry, view.Version, view.Activity)
}

func (r *RuntimeRegistry) publishRuntimeMainViewLocked(entry *authorityRuntimeEntry, version *runtimepb.ReadModelVersion, activity *runtimepb.Activity) error {
	view, err := runtimeview.MainViewFromRuntimeActivity(entry.engine, version, cloneRuntimeActivity(activity))
	if err != nil {
		return err
	}
	entry.mu.Lock()
	ready := entry.lifecycle == authorityRuntimeEntryReady
	entry.mu.Unlock()
	if !ready {
		return nil
	}
	view = cloneRuntimeMainView(view)
	entry.mainView.Store(view)
	return nil
}

func cloneRuntimeMainView(view *runtimepb.MainView) *runtimepb.MainView {
	return proto.Clone(view).(*runtimepb.MainView)
}

func cloneRuntimeActivity(activity *runtimepb.Activity) *runtimepb.Activity {
	return proto.Clone(activity).(*runtimepb.Activity)
}
