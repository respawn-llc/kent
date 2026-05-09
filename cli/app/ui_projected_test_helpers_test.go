package app

import (
	"builder/server/runtime"
	"builder/server/runtimecontrol"
	"builder/server/runtimeview"
	"builder/server/sessionview"
	"builder/shared/client"
	"builder/shared/clientui"
)

func closedRuntimeEvents() <-chan runtime.Event {
	ch := make(chan runtime.Event)
	close(ch)
	return ch
}

func closedProjectedRuntimeEvents() <-chan clientui.Event {
	ch := make(chan clientui.Event)
	close(ch)
	return ch
}

func newProjectedTestUIModel(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent, opts ...UIOption) *uiModel {
	if runtimeEvents == nil {
		runtimeEvents = make(chan clientui.Event)
	}
	if askEvents == nil {
		askEvents = make(chan askEvent)
	}
	return NewProjectedUIModel(runtimeClient, runtimeEvents, askEvents, opts...).(*uiModel)
}

func newProjectedStaticUIModel(opts ...UIOption) *uiModel {
	return newProjectedTestUIModel(nil, nil, nil, opts...)
}

func newProjectedEngineUIModel(engine *runtime.Engine, opts ...UIOption) *uiModel {
	return newProjectedTestUIModel(newUIRuntimeClient(engine), nil, nil, opts...)
}

func newUIRuntimeClientFromEngine(engine *runtime.Engine) clientui.RuntimeClient {
	if engine == nil {
		return nil
	}
	resolver := sessionview.NewStaticRuntimeResolver(engine)
	reads := client.NewLoopbackSessionViewClient(sessionview.NewService(nil, resolver, nil))
	controls := client.NewLoopbackRuntimeControlClient(runtimecontrol.NewService(resolver, nil))
	runtimeClient := newUIRuntimeClientWithReads(engine.SessionID(), reads, controls).(*sessionRuntimeClient)
	runtimeClient.storeMainView(runtimeview.MainViewFromRuntime(engine))
	return runtimeClient
}

func newUIRuntimeClient(engine *runtime.Engine) clientui.RuntimeClient {
	return newUIRuntimeClientFromEngine(engine)
}
