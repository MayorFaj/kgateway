//go:build ignore

package tests

import (
	"github.com/kgateway-dev/kgateway/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/test/kubernetes/e2e/features/validation/transformation_validation_disabled"
)

func DisableTransformationValidationSuiteRunner() e2e.SuiteRunner {
	validationSuiteRunner := e2e.NewSuiteRunner(false)

	validationSuiteRunner.Register("TransformationValidationDisabled", transformation_validation_disabled.NewTestingSuite)

	return validationSuiteRunner
}
