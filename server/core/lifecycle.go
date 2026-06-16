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

// BundleResourceRequiredError reports that a required resource for a server
// bundle was not supplied. It carries the bundle and resource names so callers
// match the specific missing dependency with errors.As instead of parsing the
// rendered message.
type BundleResourceRequiredError struct {
	BundleName   string
	ResourceName string
}

func (e BundleResourceRequiredError) Error() string {
	return fmt.Sprintf("%s bundle: %s is required", e.BundleName, e.ResourceName)
}

func bundleResourceRequiredError(bundleName string, resourceName string) error {
	return BundleResourceRequiredError{BundleName: bundleName, ResourceName: resourceName}
}
