package load_testing

import (
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	apiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/fsutils"
	e2edefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests/base"
)

var (
	// manifests
	setupManifest = filepath.Join(fsutils.MustGetThisDir(), "testdata", "setup.yaml")

	// objects
	nginxPod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: "default",
		},
	}

	testGateway = &apiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw1",
			Namespace: "default",
		},
	}

	// Use the correct proxy resource names that match what kgateway creates
	// Based on the Gateway name "gw1", kgateway creates resources with name "gw1"
	proxyObjectMeta = metav1.ObjectMeta{
		Name:      "gw1",
		Namespace: "default",
	}

	proxyDeployment     = &appsv1.Deployment{ObjectMeta: proxyObjectMeta}
	proxyService        = &corev1.Service{ObjectMeta: proxyObjectMeta}
	proxyServiceAccount = &corev1.ServiceAccount{ObjectMeta: proxyObjectMeta}

	setupTestCase = base.SimpleTestCase{
		Manifests: []string{setupManifest, e2edefaults.CurlPodManifest},
		Resources: []client.Object{proxyDeployment, proxyService, proxyServiceAccount, nginxPod, testGateway, e2edefaults.CurlPod},
	}

	testCases = map[string]*base.TestCase{
		"TestAttachedRoutes": {
			SimpleTestCase: base.SimpleTestCase{},
		},
		"TestAttachedRoutesMultiGW": {
			SimpleTestCase: base.SimpleTestCase{},
		},
		// "TestRouteProbe": {
		// 	SimpleTestCase: base.SimpleTestCase{
		// 		// No additional resources needed - test creates/deletes routes dynamically
		// 	},
		// },
		// "TestRouteChange": {
		// 	SimpleTestCase: base.SimpleTestCase{
		// 		// No additional resources needed - test creates/deletes routes dynamically
		// 	},
		// },
	}
)
