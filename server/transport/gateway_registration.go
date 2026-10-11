package transport

import (
	"fmt"

	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
)

type gatewayRegistration struct {
	operations map[string]protoapi.Operation
	binary     map[string]gatewayBinaryBinding
}

func productionGatewayRegistration() (gatewayRegistration, error) {
	operations, err := protoapi.Operations()
	if err != nil {
		return gatewayRegistration{}, err
	}
	registration := gatewayRegistration{
		operations: make(map[string]protoapi.Operation, len(operations)),
	}
	for _, operation := range operations {
		if _, duplicate := registration.operations[operation.Name]; duplicate {
			return gatewayRegistration{}, fmt.Errorf("duplicate descriptor operation %q", operation.Name)
		}
		registration.operations[operation.Name] = operation
	}
	registration.binary, err = productionGatewayBinaryBindings()
	return registration, err
}

func (r gatewayRegistration) Validate() error {
	for name, operation := range r.operations {
		binding, exists := r.binary[name]
		if operation.Options.Kind == sharedpb.OperationKind_OPERATION_KIND_NOTIFICATION {
			if exists || operation.Options.Direction != sharedpb.Direction_DIRECTION_SERVER_TO_CLIENT {
				return fmt.Errorf("notification %q has invalid direction or request binding", name)
			}
			continue
		}
		if !exists {
			return fmt.Errorf("operation %q has no binding", name)
		}
		if err := validateBinaryBinding(operation, binding); err != nil {
			return err
		}
	}
	for name := range r.binary {
		if _, exists := r.operations[name]; !exists {
			return fmt.Errorf("binding %q has no descriptor operation", name)
		}
	}
	return nil
}

func (r gatewayRegistration) BinaryBinding(operation string) (gatewayBinaryBinding, bool) {
	binding, exists := r.binary[operation]
	return binding, exists
}

func validateBinaryBinding(operation protoapi.Operation, binding gatewayBinaryBinding) error {
	if binding.operation.Name != operation.Name ||
		binding.operation.Descriptor.FullName() != operation.Descriptor.FullName() {
		return fmt.Errorf("binary binding %q has a mismatched method descriptor", operation.Name)
	}
	if binding.request == nil {
		return fmt.Errorf("binary binding %q has no request constructor", operation.Name)
	}
	switch binding.policy {
	case gatewayBinaryPreCoreOrdinary,
		gatewayBinaryPreCoreExclusive,
		gatewayBinaryCoreActiveOrdinary,
		gatewayBinaryCoreActiveExclusive:
	default:
		return fmt.Errorf("binary binding %q has no execution policy", operation.Name)
	}
	request := binding.request()
	if request == nil || request.ProtoReflect().Descriptor().FullName() != operation.Descriptor.Input().FullName() {
		return fmt.Errorf("binary binding %q has a mismatched request type", operation.Name)
	}
	if binding.failure == nil {
		return fmt.Errorf("binary binding %q has no failure mapper", operation.Name)
	}
	switch operation.Options.Kind {
	case sharedpb.OperationKind_OPERATION_KIND_UNARY:
		if binding.invoke == nil || binding.subscribe != nil || binding.associated != nil || binding.progressEvent != nil {
			return fmt.Errorf("binary unary binding %q has invalid handlers", operation.Name)
		}
	case sharedpb.OperationKind_OPERATION_KIND_SUBSCRIPTION:
		if binding.invoke != nil || binding.subscribe == nil || binding.associated == nil ||
			binding.start == nil || binding.complete == nil || binding.progressEvent != nil {
			return fmt.Errorf("binary subscription binding %q has invalid handlers", operation.Name)
		}
	case sharedpb.OperationKind_OPERATION_KIND_PROGRESS:
		if binding.invoke == nil || binding.progressEvent == nil || binding.subscribe != nil || binding.associated != nil {
			return fmt.Errorf("binary progress binding %q has invalid handlers", operation.Name)
		}
	default:
		return fmt.Errorf("binary binding %q has unsupported operation kind %s", operation.Name, operation.Options.Kind)
	}
	return nil
}
