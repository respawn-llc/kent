package client

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func bootstrapMethod(
	file protoreflect.FileDescriptor,
	serviceName protoreflect.Name,
	methodName protoreflect.Name,
) protoreflect.MethodDescriptor {
	service := file.Services().ByName(serviceName)
	if service == nil {
		panic(fmt.Sprintf("generated %s descriptor is required", serviceName))
	}
	method := service.Methods().ByName(methodName)
	if method == nil {
		panic(fmt.Sprintf("generated %s.%s descriptor is required", serviceName, methodName))
	}
	return method
}

func generatedOperationFailure(code string) error {
	if code == "" {
		return errors.New("server operation failed")
	}
	return fmt.Errorf("server operation failed with code %q", code)
}
