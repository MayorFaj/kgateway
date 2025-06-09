package performance

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/kgateway-dev/kgateway/v2/test/testutils"
)

// KgatewayLoadTestConfig for testing kgateway performance
type KgatewayLoadTestConfig struct {
	LoadTestConfig
	GatewayURL     string // URL of deployed kgateway instance
	BackendService string // Backend service to route to
	TestRoute      string // HTTP route path to test
	// TLS configuration for HTTPS testing
	TLSConfig     *tls.Config
	SkipTLSVerify bool
}

// NewKgatewayLoadTestRunner creates a load test runner for kgateway
func NewKgatewayLoadTestRunner(config KgatewayLoadTestConfig, logger *zap.Logger) *LoadTestRunner {
	return NewLoadTestRunner(config.LoadTestConfig, logger)
}

var _ = ginkgo.Describe("kgateway Load Tests", ginkgo.Label("performance", "load", "kgateway"), func() {
	var (
		logger *zap.Logger
		ctx    context.Context
	)

	ginkgo.BeforeEach(func() {
		// Skip on CI for PRs, only run on scheduled performance tests
		if testutils.IsEnvTruthy("CI") && !testutils.IsEnvTruthy("RUN_PERFORMANCE_TESTS") {
			ginkgo.Skip("Performance tests are only run on scheduled CI builds")
		}

		logger, _ = zap.NewDevelopment()
		ctx = context.Background()
	})

	ginkgo.Context("kgateway HTTP Route Performance", func() {
		ginkgo.It("should handle basic HTTP routing under load", func() {
			// This tests actual kgateway routing performance
			// Assumes kgateway is running and httpbin is deployed
			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "kgateway HTTP Route Load Test",
					TargetURL:     "http://localhost:8080/headers", // Gateway URL with httpbin endpoint
					Method:        "GET",
					Headers:       map[string]string{"Host": "www.example.com"}, // Required hostname
					QPS:           100,
					Connections:   25,
					Duration:      30 * time.Second,
					MaxErrorRate:  0.05,
					MaxP99Latency: 500 * time.Millisecond,
					MinThroughput: 90,
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "httpbin",
				TestRoute:      "/headers",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Validate kgateway performance
			gomega.Expect(result.ActualQPS).To(gomega.BeNumerically(">=", config.MinThroughput))
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", config.MaxErrorRate))
			gomega.Expect(result.LatencyP99).To(gomega.BeNumerically("<=", config.MaxP99Latency))

			logger.Info("kgateway HTTP routing performance",
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Duration("p99_latency", result.LatencyP99),
				zap.Float64("error_rate_percent", result.ErrorRate*100))
		})
	})

	ginkgo.Context("kgateway Stress Testing", func() {
		ginkgo.It("should handle very high QPS load testing", func() {
			ginkgo.By("Running very high QPS HTTP load test")

			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "Very High QPS Load Test",
					TargetURL:     "http://localhost:8080/get",
					Method:        "GET",
					Headers:       map[string]string{"Host": "www.example.com"},
					QPS:           600,
					Connections:   200,
					Duration:      15 * time.Second,
					MaxErrorRate:  0.2,              // 20% error rate tolerance for very high load
					MaxP99Latency: 15 * time.Second, // Very high tolerance for stress test
					MinThroughput: 300,              // 300 RPS minimum
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "httpbin",
				TestRoute:      "/get",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Validation for stress test
			gomega.Expect(result.TotalRequests).To(gomega.BeNumerically(">", 3000))
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", 0.3)) // 30% max error rate

			logger.Info("Very high QPS stress test completed",
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Float64("error_rate_percent", result.ErrorRate*100),
				zap.Duration("p99_latency", result.LatencyP99))
		})

		ginkgo.It("should handle connection stress testing", func() {
			ginkgo.By("Running connection stress test with many concurrent connections")

			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "Connection Stress Test",
					TargetURL:     "http://localhost:8080/get",
					Method:        "GET",
					Headers:       map[string]string{"Host": "www.example.com"},
					QPS:           200,
					Connections:   500, // Very high connection count
					Duration:      20 * time.Second,
					MaxErrorRate:  0.15,             // 15% error rate tolerance
					MaxP99Latency: 10 * time.Second, // High tolerance for connection stress
					MinThroughput: 150,              // 150 RPS minimum
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "httpbin",
				TestRoute:      "/get",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Validation for connection stress
			gomega.Expect(result.TotalRequests).To(gomega.BeNumerically(">", 2000))
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", 0.25)) // 25% max error rate

			logger.Info("Connection stress test completed",
				zap.Int("connections", 500),
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Float64("error_rate_percent", result.ErrorRate*100))
		})

		ginkgo.It("should handle burst load testing", func() {
			ginkgo.By("Running burst load test with variable QPS")

			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "Burst Load Test",
					TargetURL:     "http://localhost:8080/get",
					Method:        "GET",
					Headers:       map[string]string{"Host": "www.example.com"},
					QPS:           300,
					Connections:   100,
					Duration:      10 * time.Second, // Short burst
					MaxErrorRate:  0.1,              // 10% error rate tolerance
					MaxP99Latency: 5 * time.Second,  // Higher tolerance for burst
					MinThroughput: 200,              // 200 RPS minimum
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "httpbin",
				TestRoute:      "/get",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Validation for burst test
			gomega.Expect(result.TotalRequests).To(gomega.BeNumerically(">", 1500))
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", 0.2)) // 20% max error rate

			logger.Info("Burst load test completed",
				zap.Duration("duration", result.Duration),
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Float64("error_rate_percent", result.ErrorRate*100))
		})
	})

	ginkgo.Context("kgateway Rate Limiting Performance", func() {
		ginkgo.It("should enforce rate limits accurately under load", func() {
			ginkgo.Skip("Rate limiting test requires rate-limited route configuration")

			// This would test kgateway's rate limiting under load
			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "kgateway Rate Limiting Test",
					TargetURL:     "http://localhost:8080/rate-limited",
					Method:        "GET",
					QPS:           200, // Above rate limit
					Connections:   50,
					Duration:      30 * time.Second,
					MaxErrorRate:  0.6, // Expect high error rate due to rate limiting
					MaxP99Latency: 1 * time.Second,
					MinThroughput: 80, // Below QPS due to rate limiting
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "httpbin",
				TestRoute:      "/rate-limited",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Rate limiting should cause controlled failures
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically(">=", 0.4)) // At least 40% rate limited
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", 0.7)) // At most 70% rate limited

			logger.Info("kgateway rate limiting performance",
				zap.Float64("target_qps", 200.0),
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Float64("rate_limited_percent", result.ErrorRate*100))
		})
	})

	ginkgo.Context("kgateway TLS Performance", func() {
		ginkgo.It("should handle TLS termination under load", func() {
			ginkgo.Skip("TLS test requires HTTPS-enabled kgateway configuration")

			// This would test kgateway TLS performance
			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "kgateway TLS Performance Test",
					TargetURL:     "https://localhost:8443/get",
					Method:        "GET",
					QPS:           200,
					Connections:   75,
					Duration:      30 * time.Second,
					MaxErrorRate:  0.05,
					MaxP99Latency: 1 * time.Second,
					MinThroughput: 180,
				},
				GatewayURL:     "https://localhost:8443",
				BackendService: "httpbin",
				TestRoute:      "/get",
				SkipTLSVerify:  true,
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// TLS performance validation
			gomega.Expect(result.ActualQPS).To(gomega.BeNumerically(">=", 180))
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically("<=", 0.05))

			logger.Info("kgateway TLS performance",
				zap.Float64("actual_qps", result.ActualQPS),
				zap.Duration("p99_latency", result.LatencyP99))
		})
	})

	ginkgo.Context("kgateway Backend Health Checking", func() {
		ginkgo.It("should handle backend failures gracefully", func() {
			ginkgo.Skip("Backend health test requires controllable backend services")

			// This would test kgateway's behavior when backends fail
			config := KgatewayLoadTestConfig{
				LoadTestConfig: LoadTestConfig{
					TestName:      "kgateway Backend Health Test",
					TargetURL:     "http://localhost:8080/status/500",
					Method:        "GET",
					QPS:           100,
					Connections:   25,
					Duration:      30 * time.Second,
					MaxErrorRate:  0.8, // High error rate expected
					MaxP99Latency: 2 * time.Second,
					MinThroughput: 20, // Low throughput due to failures
				},
				GatewayURL:     "http://localhost:8080",
				BackendService: "failing-service",
				TestRoute:      "/status/500",
			}

			runner := NewKgatewayLoadTestRunner(config, logger)
			result, err := runner.RunLoadTest(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Backend failure handling validation
			gomega.Expect(result.ErrorRate).To(gomega.BeNumerically(">=", 0.7))

			logger.Info("kgateway backend health performance",
				zap.Float64("error_rate_percent", result.ErrorRate*100))
		})
	})
})
