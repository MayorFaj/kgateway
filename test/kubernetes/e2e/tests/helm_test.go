//go:build ignore

package tests_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kgateway-dev/kgateway/pkg/utils/envutils"
	"github.com/kgateway-dev/kgateway/test/kubernetes/e2e"
	. "github.com/kgateway-dev/kgateway/test/kubernetes/e2e/tests"
	"github.com/kgateway-dev/kgateway/test/kubernetes/testutils/gloogateway"
	"github.com/kgateway-dev/kgateway/test/testutils"
)

// TestHelm is the function which executes a series of helm tests
func TestHelm(t *testing.T) {
	ctx := context.Background()
	installNs, nsEnvPredefined := envutils.LookupOrDefault(testutils.InstallNamespace, "helm-test")
	testInstallation := e2e.CreateTestInstallation(
		t,
		&gloogateway.Context{
			InstallNamespace:          installNs,
			ProfileValuesManifestFile: e2e.EdgeGatewayProfilePath,
			ValuesManifestFile:        e2e.ManifestPath("test-helm.yaml"),
		},
	)

	testHelper := e2e.MustTestHelper(ctx, testInstallation)

	// Set the env to the install namespace if it is not already set
	if !nsEnvPredefined {
		os.Setenv(testutils.InstallNamespace, installNs)
	}

	// We register the cleanup function _before_ we actually perform the installation.
	// This allows us to uninstall Gloo Gateway, in case the original installation only completed partially
	t.Cleanup(func() {
		if !nsEnvPredefined {
			os.Unsetenv(testutils.InstallNamespace)
		}
		if t.Failed() {
			testInstallation.PreFailHandler(ctx)
		}

		testInstallation.UninstallGlooGatewayWithTestHelper(ctx, testHelper)
	})

	testInstallation.InstallGlooGatewayWithTestHelper(ctx, testHelper, 5*time.Minute)

	HelmSuiteRunner().Run(ctx, t, testInstallation)
}

// TestHelmSettings is the function which executes a series tests for our features/helm_settings
// See features/helm_settings/suite.go for the reason we split these tests from our standard helm tests
func TestHelmSettings(t *testing.T) {
	ctx := context.Background()
	installNs, nsEnvPredefined := envutils.LookupOrDefault(testutils.InstallNamespace, "helm-settings-test")
	testInstallation := e2e.CreateTestInstallation(
		t,
		&gloogateway.Context{
			InstallNamespace:          installNs,
			ProfileValuesManifestFile: e2e.EdgeGatewayProfilePath,
			ValuesManifestFile:        e2e.ManifestPath("test-helm.yaml"),
		},
	)

	testHelper := e2e.MustTestHelper(ctx, testInstallation)

	// Set the env to the install namespace if it is not already set
	if !nsEnvPredefined {
		os.Setenv(testutils.InstallNamespace, installNs)
	}

	// We register the cleanup function _before_ we actually perform the installation.
	// This allows us to uninstall Gloo Gateway, in case the original installation only completed partially
	t.Cleanup(func() {
		if !nsEnvPredefined {
			os.Unsetenv(testutils.InstallNamespace)
		}
		if t.Failed() {
			testInstallation.PreFailHandler(ctx)
		}

		testInstallation.UninstallGlooGatewayWithTestHelper(ctx, testHelper)
	})

	testInstallation.InstallGlooGatewayWithTestHelper(ctx, testHelper, 5*time.Minute)

	HelmSettingsSuiteRunner().Run(ctx, t, testInstallation)
}
