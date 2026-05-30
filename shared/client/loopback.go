package client

import (
	"context"
	"errors"
	"reflect"
)

type loopbackServiceHolder[S any] interface {
	loopbackService() S
}

type loopbackClient[S any] struct {
	service S
}

func newLoopbackClient[S any](service S) loopbackClient[S] {
	return loopbackClient[S]{service: service}
}

func (c loopbackClient[S]) loopbackService() S {
	return c.service
}

func callLoopbackClient[C loopbackServiceHolder[S], S any, Req any, Resp any](c C, message string, ctx context.Context, req Req, call func(S, context.Context, Req) (Resp, error)) (Resp, error) {
	service, ok := requireLoopbackService(c)
	if !ok {
		var zero Resp
		return zero, errors.New(message)
	}
	return call(service, ctx, req)
}

func callLoopbackClientNoResponse[C loopbackServiceHolder[S], S any, Req any](c C, message string, ctx context.Context, req Req, call func(S, context.Context, Req) error) error {
	service, ok := requireLoopbackService(c)
	if !ok {
		return errors.New(message)
	}
	return call(service, ctx, req)
}

func requireLoopbackService[C loopbackServiceHolder[S], S any](c C) (S, bool) {
	var zero S
	if isNilLoopbackClient(c) {
		return zero, false
	}
	service := c.loopbackService()
	if any(service) == nil {
		return zero, false
	}
	return service, true
}

func isNilLoopbackClient(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
