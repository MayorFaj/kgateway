package load_testing

import (
	"context"
	"fmt"

	// "sort"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	// "github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	// "github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	// "github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	// e2edefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests/base"
)

var _ e2e.NewSuiteFunc = NewTestingSuite

// LoadTestResults captures detailed metrics from load testing
type LoadTestResults struct {
	AttachedRoutes        *AttachedRoutesResults        `json:"attached_routes,omitempty"`
	AttachedRoutesMultiGW *AttachedRoutesMultiGWResults `json:"attached_routes_multi_gw,omitempty"`
	RouteProbe            *RouteProbeResults            `json:"route_probe,omitempty"`
	RouteChange           *RouteChangeResults           `json:"route_change,omitempty"`
}

// AttachedRoutesResults captures metrics from attached routes test
type AttachedRoutesResults struct {
	RouteCount   int           `json:"route_count"`
	SetupTime    time.Duration `json:"setup_time"`
	TeardownTime time.Duration `json:"teardown_time"`
	StatusWrites int           `json:"status_writes"`
	Samples      []Sample      `json:"samples"`
	ThresholdMet bool          `json:"threshold_met"`
}

// AttachedRoutesMultiGWResults captures metrics from multi-gateway attached routes test
type AttachedRoutesMultiGWResults struct {
	RouteCount     int                        `json:"route_count"`
	GatewayCount   int                        `json:"gateway_count"`
	SetupTime      time.Duration              `json:"setup_time"`
	TeardownTime   time.Duration              `json:"teardown_time"`
	GatewayResults map[string]*GatewayMetrics `json:"gateway_results"`
	ThresholdMet   bool                       `json:"threshold_met"`
}

// GatewayMetrics captures per-gateway performance metrics
type GatewayMetrics struct {
	GatewayName  string        `json:"gateway_name"`
	SetupTime    time.Duration `json:"setup_time"`
	TeardownTime time.Duration `json:"teardown_time"`
	StatusWrites int           `json:"status_writes"`
	Samples      []Sample      `json:"samples"`
	Efficiency   float64       `json:"efficiency"`
}

// Sample captures a single status update event
type Sample struct {
	Timestamp      time.Time     `json:"timestamp"`
	AttachedRoutes int32         `json:"attached_routes"`
	TimeSinceStart time.Duration `json:"time_since_start"`
}

// RouteProbeResults captures metrics from route probe test
type RouteProbeResults struct {
	RouteCount         int             `json:"route_count"`
	PropagationTimes   []time.Duration `json:"propagation_times"`
	AveragePropagation time.Duration   `json:"average_propagation"`
	MaxPropagation     time.Duration   `json:"max_propagation"`
	MinPropagation     time.Duration   `json:"min_propagation"`
	PercentileP95      time.Duration   `json:"percentile_p95"`
	PercentileP99      time.Duration   `json:"percentile_p99"`
	ThresholdMet       bool            `json:"threshold_met"`
}

// RouteChangeResults captures metrics from route change test
type RouteChangeResults struct {
	TestDuration   time.Duration `json:"test_duration"`
	TotalRequests  int           `json:"total_requests"`
	SuccessfulReqs int           `json:"successful_requests"`
	FailedReqs     int           `json:"failed_requests"`
	SuccessRate    float64       `json:"success_rate"`
	AverageLatency time.Duration `json:"average_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LatencyP95     time.Duration `json:"latency_p95"`
	RouteChanges   int           `json:"route_changes"`
	ThresholdMet   bool          `json:"threshold_met"`
}

const (
	attachedRoutesScaleCount = 1000
	probeRoutesScaleCount    = 500
	trafficTestDuration      = 60 * time.Second
	trafficQPS               = 100
	trafficConnections       = 10
)

// NewTestingSuite creates a new testing suite for Gateway API load testing.
func NewTestingSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	return &testingSuite{
		BaseTestingSuite: base.NewBaseTestingSuite(ctx, testInst, setupTestCase, testCases),
		results:          &LoadTestResults{},
	}
}

// testingSuite is a suite of Gateway API load tests.
type testingSuite struct {
	*base.BaseTestingSuite
	results *LoadTestResults
}

// TestAttachedRoutes measures route attachment performance with clean setup and teardown phases.
func (s *testingSuite) TestAttachedRoutes() {
	s.T().Log("Starting Attached Routes load test")

	routeCount := attachedRoutesScaleCount
	results := &AttachedRoutesResults{
		RouteCount: routeCount,
		Samples:    []Sample{},
	}

	// Clean initial state
	s.cleanupAllRoutes()
	s.waitForAttachedRoutesCount(0, 30*time.Second)

	// Start monitoring
	monitorDone := make(chan struct{})
	go s.monitorAttachedRoutes(results, monitorDone)

	// === SETUP PHASE ===
	s.T().Log("Starting route creation")
	setupStart := time.Now()
	testId := setupStart.UnixNano()

	// Create routes using bulk operations
	routes := s.createTestRoutesBulk(routeCount, testId)

	// Wait for all routes to be attached
	s.waitForAttachedRoutesCount(int32(routeCount), 60*time.Second)
	results.SetupTime = time.Since(setupStart)

	// === TEARDOWN PHASE ===
	s.T().Log("Starting route cleanup")
	teardownStart := time.Now()

	s.deleteTestRoutesBulk(routes)
	s.waitForAttachedRoutesCount(0, 60*time.Second)
	results.TeardownTime = time.Since(teardownStart)

	// Stop monitoring
	close(monitorDone)

	// Calculate metrics
	results.StatusWrites = len(results.Samples)
	results.ThresholdMet = results.SetupTime < 60*time.Second && results.TeardownTime < 60*time.Second

	s.results.AttachedRoutes = results
	s.reportAttachedRoutesResults(results)

	// Assertions
	s.Require().True(results.ThresholdMet, "Performance thresholds should be met")
	s.Require().Greater(results.StatusWrites, 0, "Should capture status updates")
}

// TestAttachedRoutesMultiGW measures route attachment performance with multiple gateways
// Each route references ALL gateways simultaneously (true simultaneous testing)
func (s *testingSuite) TestAttachedRoutesMultiGW() {
	s.T().Log("Starting Multi-Gateway Attached Routes load test")

	routeCount := 1000
	gatewayNames := []string{"kgateway-gw1", "kgateway-gw2", "kgateway-gw3"} // Different implementations
	results := &AttachedRoutesMultiGWResults{
		RouteCount:     routeCount,
		GatewayCount:   len(gatewayNames),
		GatewayResults: make(map[string]*GatewayMetrics),
	}

	// Clean initial state
	s.cleanupAllRoutes()
	s.cleanupAllGateways()

	// Create multiple gateway implementations
	s.T().Logf("Creating %d gateway implementations", len(gatewayNames))
	for _, gwName := range gatewayNames {
		s.createTestGateway(gwName)
		s.waitForGatewayReady(gwName)

		// Initialize metrics for each gateway
		results.GatewayResults[gwName] = &GatewayMetrics{
			GatewayName: gwName,
			Samples:     []Sample{},
		}
	}

	// Wait for all gateways to have 0 attached routes
	for _, gwName := range gatewayNames {
		s.waitForAttachedRoutesCountForGateway(gwName, 0, 30*time.Second)
	}

	// Start monitoring all gateways
	monitorDone := make(chan struct{})
	go s.monitorAttachedRoutesMultiGW(results, gatewayNames, monitorDone)

	// === SETUP PHASE ===
	s.T().Log("Starting route creation - each route references ALL gateways")
	setupStart := time.Now()
	testId := setupStart.UnixNano()

	// Create routes using bulk operations
	routes := s.createTestRoutesMultiGWBulk(routeCount, testId, gatewayNames)

	// Wait for all gateways to show the full route count
	s.T().Logf("Waiting for all %d gateways to attach all %d routes", len(gatewayNames), routeCount)
	for _, gwName := range gatewayNames {
		s.waitForAttachedRoutesCountForGateway(gwName, int32(routeCount), 5*time.Minute)
	}
	results.SetupTime = time.Since(setupStart)

	// === TEARDOWN PHASE ===
	s.T().Log("Starting route cleanup for all gateways")
	teardownStart := time.Now()

	s.deleteTestRoutesBulk(routes)

	// Wait for all gateways to show 0 attached routes
	for _, gwName := range gatewayNames {
		s.waitForAttachedRoutesCountForGateway(gwName, 0, 5*time.Minute)
	}
	results.TeardownTime = time.Since(teardownStart)

	// Stop monitoring
	close(monitorDone)

	// Calculate final metrics for each gateway
	for _, metrics := range results.GatewayResults {
		metrics.SetupTime = results.SetupTime
		metrics.TeardownTime = results.TeardownTime
		metrics.StatusWrites = len(metrics.Samples)
		if metrics.StatusWrites > 0 {
			metrics.Efficiency = float64(routeCount) / float64(metrics.StatusWrites)
		}
	}

	// Calculate overall metrics
	results.ThresholdMet = results.SetupTime < 120*time.Second && results.TeardownTime < 120*time.Second

	s.results.AttachedRoutesMultiGW = results
	s.reportAttachedRoutesMultiGWResults(results)

	// Assertions
	s.Require().True(results.ThresholdMet, "Performance thresholds should be met")
	s.T().Logf("Test completed - compared %d gateway implementations with %d routes each", len(gatewayNames), routeCount)
}

// // TestRouteProbe measures route propagation timing
// func (s *testingSuite) TestRouteProbe() {
// 	s.T().Log("Starting Route Probe test")

// 	routeCount := 50
// 	results := &RouteProbeResults{
// 		RouteCount:       routeCount,
// 		PropagationTimes: make([]time.Duration, 0, routeCount),
// 	}

// 	testId := time.Now().UnixNano()

// 	for i := 0; i < routeCount; i++ {
// 		startTime := time.Now()
// 		routeName := fmt.Sprintf("probe-route-%d-%d", testId, i)

// 		// Create route
// 		route := s.createTestHTTPRoute(routeName, "default")
// 		err := s.TestInstallation.ClusterContext.Client.Create(s.Ctx, route)
// 		s.Require().NoError(err)

// 		// Wait for route to accept traffic
// 		s.waitForRouteReady(route, 30*time.Second)

// 		propagationTime := time.Since(startTime)
// 		results.PropagationTimes = append(results.PropagationTimes, propagationTime)

// 		// Cleanup for next iteration
// 		err = s.TestInstallation.ClusterContext.Client.Delete(s.Ctx, route)
// 		s.Require().NoError(err)
// 		s.TestInstallation.Assertions.EventuallyObjectsNotExist(s.Ctx, route)

// 		time.Sleep(100 * time.Millisecond)
// 	}

// 	// Calculate statistics
// 	results.AveragePropagation = s.calculateAverage(results.PropagationTimes)
// 	results.MaxPropagation = s.calculateMax(results.PropagationTimes)
// 	results.MinPropagation = s.calculateMin(results.PropagationTimes)
// 	results.PercentileP95 = s.calculatePercentile(results.PropagationTimes, 95)
// 	results.PercentileP99 = s.calculatePercentile(results.PropagationTimes, 99)
// 	results.ThresholdMet = results.AveragePropagation < 2*time.Second

// 	s.results.RouteProbe = results
// 	s.reportRouteProbeResults(results)

// 	s.Require().True(results.ThresholdMet, "Propagation time threshold should be met")
// }

// // TestRouteChange measures traffic continuity during route changes
// func (s *testingSuite) TestRouteChange() {
// 	s.T().Log("Starting Route Change test")

// 	results := &RouteChangeResults{
// 		TestDuration: trafficTestDuration,
// 	}

// 	// Create initial route
// 	route := s.createTestHTTPRoute("change-test-route", "default")
// 	err := s.TestInstallation.ClusterContext.Client.Create(s.Ctx, route)
// 	s.Require().NoError(err)

// 	s.waitForRouteReady(route, 30*time.Second)

// 	// Start traffic generation
// 	trafficDone := make(chan struct{})
// 	var latencies []time.Duration
// 	go s.generateTraffic(results, &latencies, trafficDone)

// 	// Apply route changes periodically
// 	startTime := time.Now()
// 	changeCount := 0

// 	for time.Since(startTime) < trafficTestDuration {
// 		time.Sleep(5 * time.Second)

// 		// Modify route
// 		route.Spec.Rules[0].BackendRefs[0].BackendRef.BackendObjectReference.Name = "nginx"
// 		err := s.TestInstallation.ClusterContext.Client.Update(s.Ctx, route)
// 		if err == nil {
// 			changeCount++
// 		}
// 	}

// 	// Wait for traffic to complete
// 	<-trafficDone

// 	// Calculate final statistics
// 	results.RouteChanges = changeCount
// 	results.SuccessRate = float64(results.SuccessfulReqs) / float64(results.TotalRequests) * 100
// 	results.AverageLatency = s.calculateAverage(latencies)
// 	results.MaxLatency = s.calculateMax(latencies)
// 	results.LatencyP95 = s.calculatePercentile(latencies, 95)
// 	results.ThresholdMet = results.SuccessRate > 95.0

// 	s.results.RouteChange = results
// 	s.reportRouteChangeResults(results)

// 	// Cleanup
// 	err = s.TestInstallation.ClusterContext.Client.Delete(s.Ctx, route)
// 	s.Require().NoError(err)

// 	s.Require().True(results.ThresholdMet, "Traffic continuity threshold should be met")
// }

// Helper methods

func (s *testingSuite) cleanupAllRoutes() {
	routes := &gwv1.HTTPRouteList{}
	err := s.TestInstallation.ClusterContext.Client.List(s.Ctx, routes, &client.ListOptions{
		Namespace: "default",
	})
	if err != nil {
		return
	}

	for _, route := range routes.Items {
		_ = s.TestInstallation.ClusterContext.Client.Delete(s.Ctx, &route)
	}

	time.Sleep(2 * time.Second)
}

func (s *testingSuite) cleanupAllGateways() {
	gateways := &gwv1.GatewayList{}
	err := s.TestInstallation.ClusterContext.Client.List(s.Ctx, gateways, &client.ListOptions{
		Namespace: "default",
	})
	if err != nil {
		return
	}

	for _, gateway := range gateways.Items {
		if gateway.Name != "gw1" { // Keep the default gateway
			_ = s.TestInstallation.ClusterContext.Client.Delete(s.Ctx, &gateway)
		}
	}

	time.Sleep(2 * time.Second)
}

func (s *testingSuite) createTestRoutesBulk(count int, testId int64) []*gwv1.HTTPRoute {
	routes := make([]*gwv1.HTTPRoute, count)

	// Create all route objects first (in memory)
	for i := 0; i < count; i++ {
		routeName := fmt.Sprintf("test-route-%d-%d", testId, i)
		routes[i] = s.createTestHTTPRoute(routeName, "default")
	}

	// Apply all routes in a single bulk operation using server-side apply
	s.T().Logf("Applying %d routes in bulk operation", count)

	// Use goroutines to parallelize the API calls (similar to simulation framework)
	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, count)

	for i, route := range routes {
		sem <- struct{}{} // Acquire semaphore
		go func(route *gwv1.HTTPRoute, index int) {
			defer func() { <-sem }() // Release semaphore

			if err := s.TestInstallation.ClusterContext.Client.Create(s.Ctx, route); err != nil {
				errChan <- fmt.Errorf("failed to create route %d: %w", index, err)
				return
			}
			errChan <- nil
		}(route, i)
	}

	// Wait for all operations to complete
	for i := 0; i < count; i++ {
		if err := <-errChan; err != nil {
			s.Require().NoError(err)
		}
	}

	s.T().Logf("Successfully created %d routes using bulk operation", count)
	return routes
}

func (s *testingSuite) createTestRoutesMultiGWBulk(count int, testId int64, gatewayNames []string) []*gwv1.HTTPRoute {
	routes := make([]*gwv1.HTTPRoute, count)

	// Create all route objects first (in memory)
	for i := 0; i < count; i++ {
		routeName := fmt.Sprintf("test-route-multi-%d-%d", testId, i)
		routes[i] = s.createTestHTTPRouteMultiGW(routeName, "default", gatewayNames)
	}

	// Apply all routes in parallel (simulation framework pattern)
	s.T().Logf("Applying %d multi-gateway routes in bulk operation", count)

	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, count)

	for i, route := range routes {
		sem <- struct{}{} // Acquire semaphore
		go func(route *gwv1.HTTPRoute, index int) {
			defer func() { <-sem }() // Release semaphore

			if err := s.TestInstallation.ClusterContext.Client.Create(s.Ctx, route); err != nil {
				errChan <- fmt.Errorf("failed to create multi-gw route %d: %w", index, err)
				return
			}
			errChan <- nil
		}(route, i)
	}

	// Wait for all operations to complete
	for i := 0; i < count; i++ {
		if err := <-errChan; err != nil {
			s.Require().NoError(err)
		}
	}

	s.T().Logf("Successfully created %d multi-gateway routes using bulk operation", count)
	return routes
}

func (s *testingSuite) deleteTestRoutesBulk(routes []*gwv1.HTTPRoute) {
	s.T().Logf("Deleting %d routes in bulk operation", len(routes))

	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, len(routes))

	for i, route := range routes {
		sem <- struct{}{} // Acquire semaphore
		go func(route *gwv1.HTTPRoute, index int) {
			defer func() { <-sem }() // Release semaphore

			if err := s.TestInstallation.ClusterContext.Client.Delete(s.Ctx, route); err != nil {
				// Deletion errors are often expected (resource may already be gone)
				errChan <- nil
				return
			}
			errChan <- nil
		}(route, i)
	}

	// Wait for all operations to complete
	for i := 0; i < len(routes); i++ {
		<-errChan
	}

	s.T().Logf("Successfully deleted %d routes using bulk operation", len(routes))
}

func (s *testingSuite) createTestHTTPRoute(name, namespace string) *gwv1.HTTPRoute {
	return &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{
				ParentRefs: []gwv1.ParentReference{
					{
						Name: "gw1",
					},
				},
			},
			Rules: []gwv1.HTTPRouteRule{
				{
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name: "nginx",
									Port: &[]gwv1.PortNumber{80}[0],
								},
							},
						},
					},
				},
			},
		},
	}
}

func (s *testingSuite) createTestGateway(name string) *gwv1.Gateway {
	gateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: "kgateway",
			Listeners: []gwv1.Listener{
				{
					Name:     "http",
					Protocol: gwv1.HTTPProtocolType,
					Port:     8080,
				},
			},
		},
	}

	err := s.TestInstallation.ClusterContext.Client.Create(s.Ctx, gateway)
	s.Require().NoError(err)
	return gateway
}

func (s *testingSuite) getGatewayByName(name string) *gwv1.Gateway {
	gateway := &gwv1.Gateway{}
	err := s.TestInstallation.ClusterContext.Client.Get(s.Ctx, types.NamespacedName{
		Namespace: "default",
		Name:      name,
	}, gateway)
	s.Require().NoError(err)
	return gateway
}

func (s *testingSuite) getAttachedRoutesCountForGateway(gatewayName string) int32 {
	gateway := s.getGatewayByName(gatewayName)
	if len(gateway.Status.Listeners) == 0 {
		return 0
	}
	return gateway.Status.Listeners[0].AttachedRoutes
}

func (s *testingSuite) createTestHTTPRouteMultiGW(name, namespace string, gatewayNames []string) *gwv1.HTTPRoute {
	var parentRefs []gwv1.ParentReference
	for _, gwName := range gatewayNames {
		parentRefs = append(parentRefs, gwv1.ParentReference{
			Name: gwv1.ObjectName(gwName),
		})
	}

	return &gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: []gwv1.HTTPRouteRule{
				{
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name: "nginx",
									Port: &[]gwv1.PortNumber{80}[0],
								},
							},
						},
					},
				},
			},
		},
	}
}

func (s *testingSuite) getGateway() *gwv1.Gateway {
	gateway := &gwv1.Gateway{}
	err := s.TestInstallation.ClusterContext.Client.Get(s.Ctx, types.NamespacedName{
		Namespace: "default",
		Name:      "gw1",
	}, gateway)
	s.Require().NoError(err)
	return gateway
}

func (s *testingSuite) getAttachedRoutesCount() int32 {
	gateway := s.getGateway()
	if len(gateway.Status.Listeners) == 0 {
		return 0
	}
	return gateway.Status.Listeners[0].AttachedRoutes
}

func (s *testingSuite) waitForAttachedRoutesCount(targetCount int32, timeout time.Duration) {
	gomega.Eventually(func() int32 {
		return s.getAttachedRoutesCount()
	}, timeout, 500*time.Millisecond).Should(gomega.Equal(targetCount))
}

func (s *testingSuite) waitForGatewayReady(gatewayName string) {
	gomega.Eventually(func() bool {
		gateway := s.getGatewayByName(gatewayName)
		return len(gateway.Status.Listeners) > 0
	}, 30*time.Second, 500*time.Millisecond).Should(gomega.BeTrue())
}

func (s *testingSuite) waitForAttachedRoutesCountForGateway(gatewayName string, targetCount int32, timeout time.Duration) {
	gomega.Eventually(func() int32 {
		return s.getAttachedRoutesCountForGateway(gatewayName)
	}, timeout, 500*time.Millisecond).Should(gomega.Equal(targetCount))
}

// func (s *testingSuite) waitForRouteReady(_ *gwv1.HTTPRoute, timeout time.Duration) {
// 	gomega.Eventually(func() bool {
// 		resp := s.TestInstallation.Assertions.AssertCurlReturnResponse(
// 			s.Ctx,
// 			e2edefaults.CurlPodExecOpt,
// 			[]curl.Option{
// 				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
// 				curl.WithHostHeader("example.com"),
// 				curl.WithPort(8080),
// 			},
// 			&matchers.HttpResponse{StatusCode: 200},
// 		)
// 		defer resp.Body.Close()
// 		return resp.StatusCode == 200
// 	}, timeout, 500*time.Millisecond).Should(gomega.BeTrue())
// }

func (s *testingSuite) monitorAttachedRoutes(results *AttachedRoutesResults, done chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()
	lastCount := int32(-1)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			currentCount := s.getAttachedRoutesCount()
			if currentCount != lastCount {
				sample := Sample{
					Timestamp:      time.Now(),
					AttachedRoutes: currentCount,
					TimeSinceStart: time.Since(startTime),
				}
				results.Samples = append(results.Samples, sample)
				lastCount = currentCount
			}
		}
	}
}

func (s *testingSuite) monitorAttachedRoutesMultiGW(results *AttachedRoutesMultiGWResults, gatewayNames []string, done chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()
	lastCounts := make(map[string]int32)

	// Initialize last counts
	for _, gwName := range gatewayNames {
		lastCounts[gwName] = -1
	}

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			for _, gwName := range gatewayNames {
				currentCount := s.getAttachedRoutesCountForGateway(gwName)
				lastCount := lastCounts[gwName]

				if currentCount != lastCount {
					metrics := results.GatewayResults[gwName]
					if metrics == nil {
						metrics = &GatewayMetrics{
							GatewayName: gwName,
							Samples:     []Sample{},
						}
						results.GatewayResults[gwName] = metrics
					}

					sample := Sample{
						Timestamp:      time.Now(),
						AttachedRoutes: currentCount,
						TimeSinceStart: time.Since(startTime),
					}
					metrics.Samples = append(metrics.Samples, sample)
					lastCounts[gwName] = currentCount
				}
			}
		}
	}
}

// func (s *testingSuite) generateTraffic(results *RouteChangeResults, latencies *[]time.Duration, done chan struct{}) {
// 	defer close(done)

// 	startTime := time.Now()

// 	for time.Since(startTime) < trafficTestDuration {
// 		reqStart := time.Now()

// 		resp := s.TestInstallation.Assertions.AssertCurlReturnResponse(
// 			s.Ctx,
// 			e2edefaults.CurlPodExecOpt,
// 			[]curl.Option{
// 				curl.WithHost(kubeutils.ServiceFQDN(proxyObjectMeta)),
// 				curl.WithHostHeader("example.com"),
// 				curl.WithPort(8080),
// 			},
// 			&matchers.HttpResponse{StatusCode: 200},
// 		)

// 		reqLatency := time.Since(reqStart)
// 		results.TotalRequests++

// 		if resp.StatusCode == 200 {
// 			results.SuccessfulReqs++
// 			*latencies = append(*latencies, reqLatency)
// 		} else {
// 			results.FailedReqs++
// 		}

// 		resp.Body.Close()
// 		time.Sleep(100 * time.Millisecond)
// 	}
// }

// Statistics calculation methods

// func (s *testingSuite) calculateAverage(durations []time.Duration) time.Duration {
// 	if len(durations) == 0 {
// 		return 0
// 	}
// 	var total time.Duration
// 	for _, d := range durations {
// 		total += d
// 	}
// 	return total / time.Duration(len(durations))
// }

// func (s *testingSuite) calculateMax(durations []time.Duration) time.Duration {
// 	if len(durations) == 0 {
// 		return 0
// 	}
// 	max := durations[0]
// 	for _, d := range durations {
// 		if d > max {
// 			max = d
// 		}
// 	}
// 	return max
// }

// func (s *testingSuite) calculateMin(durations []time.Duration) time.Duration {
// 	if len(durations) == 0 {
// 		return 0
// 	}
// 	min := durations[0]
// 	for _, d := range durations {
// 		if d < min {
// 			min = d
// 		}
// 	}
// 	return min
// }

// func (s *testingSuite) calculatePercentile(durations []time.Duration, percentile int) time.Duration {
// 	if len(durations) == 0 {
// 		return 0
// 	}

// 	sorted := make([]time.Duration, len(durations))
// 	copy(sorted, durations)
// 	sort.Slice(sorted, func(i, j int) bool {
// 		return sorted[i] < sorted[j]
// 	})

// 	index := (percentile * len(sorted)) / 100
// 	if index >= len(sorted) {
// 		index = len(sorted) - 1
// 	}
// 	return sorted[index]
// }

// Reporting methods

func (s *testingSuite) reportAttachedRoutesResults(results *AttachedRoutesResults) {
	s.T().Log("=== ATTACHED ROUTES RESULTS ===")
	s.T().Logf("Route Count: %d", results.RouteCount)
	s.T().Logf("Setup Time: %v", results.SetupTime)
	s.T().Logf("Teardown Time: %v", results.TeardownTime)
	s.T().Logf("Status Writes: %d", results.StatusWrites)
	s.T().Logf("Efficiency: %.2f routes/write", float64(results.RouteCount)/float64(results.StatusWrites))
	s.T().Logf("Threshold Met: %t", results.ThresholdMet)
}

func (s *testingSuite) reportAttachedRoutesMultiGWResults(results *AttachedRoutesMultiGWResults) {
	s.T().Log("=== MULTI-GATEWAY ATTACHED ROUTES RESULTS ===")
	s.T().Logf("Route Count: %d", results.RouteCount)
	s.T().Logf("Gateway Count: %d", results.GatewayCount)
	s.T().Logf("Overall Setup Time: %v", results.SetupTime)
	s.T().Logf("Overall Teardown Time: %v", results.TeardownTime)
	s.T().Log("")

	s.T().Log("Per-Gateway Performance:")
	for gwName, metrics := range results.GatewayResults {
		s.T().Logf("Gateway: %s", gwName)
		s.T().Logf("  Status Writes: %d", metrics.StatusWrites)
		s.T().Logf("  Efficiency: %.2f routes/write", metrics.Efficiency)
		s.T().Logf("  Setup Rate: %.2f routes/sec", float64(results.RouteCount)/metrics.SetupTime.Seconds())
		s.T().Logf("  Teardown Rate: %.2f routes/sec", float64(results.RouteCount)/metrics.TeardownTime.Seconds())
	}

	s.T().Logf("Overall Threshold Met: %t", results.ThresholdMet)
}

func (s *testingSuite) reportRouteProbeResults(results *RouteProbeResults) {
	s.T().Log("=== ROUTE PROBE RESULTS ===")
	s.T().Logf("Route Count: %d", results.RouteCount)
	s.T().Logf("Average Propagation: %v", results.AveragePropagation)
	s.T().Logf("Max Propagation: %v", results.MaxPropagation)
	s.T().Logf("Min Propagation: %v", results.MinPropagation)
	s.T().Logf("P95 Propagation: %v", results.PercentileP95)
	s.T().Logf("P99 Propagation: %v", results.PercentileP99)
	s.T().Logf("Threshold Met: %t", results.ThresholdMet)
}

func (s *testingSuite) reportRouteChangeResults(results *RouteChangeResults) {
	s.T().Log("=== ROUTE CHANGE RESULTS ===")
	s.T().Logf("Test Duration: %v", results.TestDuration)
	s.T().Logf("Total Requests: %d", results.TotalRequests)
	s.T().Logf("Success Rate: %.2f%%", results.SuccessRate)
	s.T().Logf("Average Latency: %v", results.AverageLatency)
	s.T().Logf("Max Latency: %v", results.MaxLatency)
	s.T().Logf("P95 Latency: %v", results.LatencyP95)
	s.T().Logf("Route Changes: %d", results.RouteChanges)
	s.T().Logf("Threshold Met: %t", results.ThresholdMet)
}

// GetResults returns the complete load test results
func (s *testingSuite) GetResults() *LoadTestResults {
	return s.results
}

// PrintDetailedResults outputs comprehensive test analysis
func (s *testingSuite) PrintDetailedResults() {
	s.T().Log("=== LOAD TEST ANALYSIS ===")

	if s.results.AttachedRoutes != nil {
		s.reportAttachedRoutesResults(s.results.AttachedRoutes)
	}

	if s.results.AttachedRoutesMultiGW != nil {
		s.reportAttachedRoutesMultiGWResults(s.results.AttachedRoutesMultiGW)
	}

	if s.results.RouteProbe != nil {
		s.reportRouteProbeResults(s.results.RouteProbe)
	}

	if s.results.RouteChange != nil {
		s.reportRouteChangeResults(s.results.RouteChange)
	}
}
