package performance

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	testmatchers "github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	testdefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
)

var _ e2e.NewSuiteFunc = NewTestingSuite

// testingSuite is a suite of performance and load testing tests for kgateway
type testingSuite struct {
	suite.Suite

	ctx context.Context

	// testInstallation contains all the metadata/utilities necessary to execute a series of tests
	// against an installation of kgateway
	testInstallation *e2e.TestInstallation

	// logger for performance test logging
	logger *zap.Logger
}

func NewTestingSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	logger, _ := zap.NewDevelopment()
	return &testingSuite{
		ctx:              ctx,
		testInstallation: testInst,
		logger:           logger,
	}
}

func (s *testingSuite) TestHTTPRouteLoadTest() {
	manifests := []string{
		testdefaults.CurlPodManifest,
		performanceManifest,
	}

	s.T().Cleanup(func() {
		for _, manifest := range manifests {
			err := s.testInstallation.Actions.Kubectl().DeleteFileSafe(s.ctx, manifest)
			s.Require().NoError(err)
		}
	})

	for _, manifest := range manifests {
		err := s.testInstallation.Actions.Kubectl().ApplyFile(s.ctx, manifest)
		s.Require().NoError(err)
	}

	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, "httpbin", metav1.ListOptions{
		LabelSelector: "app=httpbin",
	})

	time.Sleep(15 * time.Second)

	actualGatewayService := s.findGatewayService()
	if actualGatewayService == nil {
		s.T().Fatal("Could not find kgateway service")
	}

	s.testInstallation.Assertions.AssertEventualCurlResponse(
		s.ctx,
		testdefaults.CurlPodExecOpt,
		[]curl.Option{
			curl.WithHost(kubeutils.ServiceFQDN(actualGatewayService.ObjectMeta)),
			curl.WithHostHeader("www.example.com"),
			curl.WithPath("/headers"),
			curl.WithPort(8080),
		},
		&testmatchers.HttpResponse{
			StatusCode: http.StatusOK,
		},
	)

	// Phase 1: High Volume Stress Test
	highVolumeConfig := LoadTestConfig{
		TestName:        "High Volume Stress Test",
		ServiceFQDN:     kubeutils.ServiceFQDN(actualGatewayService.ObjectMeta),
		Path:            "/headers",
		Headers:         map[string]string{"Host": "www.example.com"},
		RequestCount:    6000,
		ConcurrentJobs:  75,
		RequestInterval: 2 * time.Millisecond,
		MaxErrorRate:    0.02,
		MinSuccessRate:  0.98,
		MaxFailureCount: 50,
	}

	highVolumeResult, err := s.runInClusterLoadTest(highVolumeConfig)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(highVolumeResult.SuccessRate, highVolumeConfig.MinSuccessRate)
	s.Require().LessOrEqual(highVolumeResult.ErrorRate, highVolumeConfig.MaxErrorRate)

	// Log Phase 1 results
	s.logger.Info("Phase 1 completed",
		zap.String("test_name", highVolumeResult.TestName),
		zap.Int("total_requests", highVolumeResult.TotalRequests),
		zap.Int("successful_requests", highVolumeResult.SuccessRequests),
		zap.Int("failed_requests", highVolumeResult.FailedRequests),
		zap.Float64("success_rate", highVolumeResult.SuccessRate),
		zap.Float64("error_rate", highVolumeResult.ErrorRate),
		zap.Float64("qps", float64(highVolumeResult.TotalRequests)/highVolumeResult.Duration.Seconds()),
		zap.Duration("duration", highVolumeResult.Duration),
		zap.Any("response_codes", highVolumeResult.ResponseCodes))

	// Phase 2: Extreme Load Test
	extremeConfig := LoadTestConfig{
		TestName:        "Extreme Load Test",
		ServiceFQDN:     kubeutils.ServiceFQDN(actualGatewayService.ObjectMeta),
		Path:            "/headers",
		Headers:         map[string]string{"Host": "www.example.com"},
		RequestCount:    10000,
		ConcurrentJobs:  150,
		RequestInterval: 1 * time.Millisecond,
		MaxErrorRate:    0.10,
		MinSuccessRate:  0.90,
		MaxFailureCount: 100,
	}

	extremeResult, err := s.runInClusterLoadTest(extremeConfig)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(extremeResult.SuccessRate, extremeConfig.MinSuccessRate)
	s.Require().LessOrEqual(extremeResult.ErrorRate, extremeConfig.MaxErrorRate)

	// Log Phase 2 results
	s.logger.Info("Phase 2 completed",
		zap.String("test_name", extremeResult.TestName),
		zap.Int("total_requests", extremeResult.TotalRequests),
		zap.Int("successful_requests", extremeResult.SuccessRequests),
		zap.Int("failed_requests", extremeResult.FailedRequests),
		zap.Float64("success_rate", extremeResult.SuccessRate),
		zap.Float64("error_rate", extremeResult.ErrorRate),
		zap.Float64("qps", float64(extremeResult.TotalRequests)/extremeResult.Duration.Seconds()),
		zap.Duration("duration", extremeResult.Duration),
		zap.Any("response_codes", extremeResult.ResponseCodes))

	totalRequests := highVolumeResult.TotalRequests + extremeResult.TotalRequests
	totalSuccessful := highVolumeResult.SuccessRequests + extremeResult.SuccessRequests
	overallSuccessRate := float64(totalSuccessful) / float64(totalRequests)

	// Summary log with less detail since individual phases are already logged
	s.logger.Info("Load test suite completed - Summary",
		zap.Int("total_requests", totalRequests),
		zap.Int("successful_requests", totalSuccessful),
		zap.Float64("overall_success_rate", overallSuccessRate))
}

func (s *testingSuite) findGatewayService() *corev1.Service {
	possibleNames := []string{"http", "kgateway", "kgateway-gateway", "gateway", "kgateway-service"}

	for _, name := range possibleNames {
		svc, err := s.testInstallation.ClusterContext.Clientset.CoreV1().Services(s.testInstallation.Metadata.InstallNamespace).Get(s.ctx, name, metav1.GetOptions{})
		if err == nil {
			s.T().Logf("Found gateway service: %s", name)
			return svc
		}
	}
	return nil
}

// runInClusterLoadTest executes a load test using in-cluster curl commands
func (s *testingSuite) runInClusterLoadTest(config LoadTestConfig) (*LoadTestResult, error) {
	startTime := time.Now()

	var wg sync.WaitGroup
	resultChan := make(chan curlResult, config.RequestCount)

	requestsPerJob := config.RequestCount / config.ConcurrentJobs
	remainingRequests := config.RequestCount % config.ConcurrentJobs

	s.logger.Info("Starting in-cluster load test",
		zap.String("test_name", config.TestName),
		zap.Int("total_requests", config.RequestCount),
		zap.Int("concurrent_jobs", config.ConcurrentJobs),
		zap.Duration("request_interval", config.RequestInterval))

	// Start concurrent curl jobs
	for i := 0; i < config.ConcurrentJobs; i++ {
		requests := requestsPerJob
		if i < remainingRequests {
			requests++
		}

		wg.Add(1)
		go func(jobID int, requestCount int) {
			defer wg.Done()
			s.runCurlJob(jobID, requestCount, config, resultChan)
		}(i, requests)
	}

	// Wait for all jobs to complete
	wg.Wait()
	close(resultChan)

	// Collect results
	result := &LoadTestResult{
		TestName:      config.TestName,
		ResponseCodes: make(map[int]int),
		SampleErrors:  make([]string, 0),
	}

	for curlRes := range resultChan {
		result.TotalRequests++

		// Always count the status code if we got one (including 429s)
		if curlRes.StatusCode > 0 {
			result.ResponseCodes[curlRes.StatusCode]++
		}

		if curlRes.Success {
			result.SuccessRequests++
		} else {
			result.FailedRequests++
			if len(result.SampleErrors) < 5 && curlRes.Error != "" {
				result.SampleErrors = append(result.SampleErrors, curlRes.Error)
			}
		}
	}

	result.Duration = time.Since(startTime)
	if result.TotalRequests > 0 {
		result.SuccessRate = float64(result.SuccessRequests) / float64(result.TotalRequests)
		result.ErrorRate = float64(result.FailedRequests) / float64(result.TotalRequests)
	}

	return result, nil
}

type curlResult struct {
	Success    bool
	StatusCode int
	Error      string
}

// runCurlJob executes multiple curl requests in sequence
func (s *testingSuite) runCurlJob(jobID, requestCount int, config LoadTestConfig, resultChan chan<- curlResult) {
	_ = jobID

	for i := 0; i < requestCount; i++ {
		args := []string{
			"exec", "-n", testdefaults.CurlPod.GetNamespace(), testdefaults.CurlPod.GetName(),
			"-c", "curl", "--",
			"curl", "-s", "-w", "%{http_code}", "-o", "/dev/null",
			"--max-time", "10",
		}

		for key, value := range config.Headers {
			args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
		}

		url := fmt.Sprintf("http://%s:8080%s", config.ServiceFQDN, config.Path)
		args = append(args, url)

		stdout, stderr, err := s.testInstallation.Actions.Kubectl().Execute(s.ctx, args...)

		result := curlResult{}

		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("curl execution failed: %v, stderr: %s", err, stderr)
		} else {
			// Parse status code from stdout
			statusStr := strings.TrimSpace(stdout)
			if statusCode, parseErr := strconv.Atoi(statusStr); parseErr == nil {
				result.StatusCode = statusCode

				result.Success = statusCode >= 200 && statusCode < 400

				if !result.Success && statusCode >= 400 {
					if statusCode == 429 {
						result.Error = "HTTP 429 (Rate Limited)"
					} else {
						result.Error = fmt.Sprintf("HTTP %d", statusCode)
					}
				}
			} else {
				result.Success = false
				result.Error = fmt.Sprintf("failed to parse status code: %s", statusStr)
			}
		}

		resultChan <- result

		if config.RequestInterval > 0 && i < requestCount-1 {
			time.Sleep(config.RequestInterval)
		}
	}
}
