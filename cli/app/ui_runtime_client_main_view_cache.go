package app

import (
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"

	"google.golang.org/protobuf/proto"
)

func (c *sessionRuntimeClient) cachedMainView() (*runtimepb.MainView, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	view := proto.Clone(c.mainView).(*runtimepb.MainView)
	if !c.hasMainView {
		return view, false
	}
	return view, true
}

func (c *sessionRuntimeClient) CachedMainView() (*runtimepb.MainView, bool) {
	if c == nil {
		return &runtimepb.MainView{}, false
	}
	return c.cachedMainView()
}

func (c *sessionRuntimeClient) storeMainView(view *runtimepb.MainView) *runtimepb.MainView {
	return c.mergeMainViewCandidate(view, runtimeTupleIngressAuthoritativeSnapshot).view
}

func (c *sessionRuntimeClient) mergeMainViewCandidate(
	view *runtimepb.MainView,
	ingress runtimeTupleIngress,
) runtimeTupleMergeResult {
	view = proto.Clone(view).(*runtimepb.MainView)
	c.mu.Lock()
	defer c.mu.Unlock()
	decision := decideRuntimeTuple(c.mainView.Version, view.Version, ingress)
	c.mainView.Status = view.Status
	c.mainView.Session = view.Session
	if decision == runtimeTupleApply {
		applyRuntimeTuple(c.mainView, runtimeTupleFromMainView(view))
	}
	c.hasMainView = true
	return runtimeTupleMergeResult{decision: decision, view: proto.Clone(c.mainView).(*runtimepb.MainView), project: decision == runtimeTupleApply}
}

func (c *sessionRuntimeClient) patchMainView(apply func(view *runtimepb.MainView)) {
	c.mu.Lock()
	apply(c.mainView)
	c.hasMainView = true
	c.mu.Unlock()
}
