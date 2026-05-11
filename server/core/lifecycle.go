package core

import (
	"errors"
	"fmt"
)

type lifecycleResource struct {
	name  string
	close func() error
}

func closeLifecycleResources(resources []lifecycleResource) error {
	var err error
	for i := len(resources) - 1; i >= 0; i-- {
		resource := resources[i]
		if resource.close == nil {
			continue
		}
		if closeErr := resource.close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("%s: %w", resource.name, closeErr))
		}
	}
	return err
}

func bundleResourceRequiredError(bundleName string, resourceName string) error {
	return fmt.Errorf("%s bundle: %s is required", bundleName, resourceName)
}
