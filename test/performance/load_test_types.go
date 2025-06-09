package performance

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/onsi/ginkgo/v2"
	"go.uber.org/zap"
)

// LoadTestConfig defines parameters for load testing
type LoadTestConfig struct {
	// Target configuration
	TargetURL string
	Method    string
	Headers   map[string]string
	Body      []byte

	// Load parameters
	QPS         int           // Queries per second (0 = unlimited)
	Connections int           // Number of concurrent connections
	Duration    time.Duration // Test duration

	// Validation thresholds
	MaxErrorRate  float64       // Maximum acceptable error rate (0.01 = 1%)
	MaxP99Latency time.Duration // Maximum acceptable P99 latency
	MinThroughput float64       // Minimum acceptable requests/second

	// Test metadata
	TestName    string
	Description string
}

// LoadTestResult contains the results of a load test
type LoadTestResult struct {
	// Core metrics
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	ActualQPS          float64
	ErrorRate          float64

	// Latency metrics (following industry standard percentiles)
	LatencyP50  time.Duration
	LatencyP95  time.Duration
	LatencyP99  time.Duration
	LatencyP999 time.Duration
	LatencyMean time.Duration
	LatencyMax  time.Duration
	LatencyMin  time.Duration

	// Test metadata
	Duration time.Duration
	TestName string
}

type LoadTestRunner struct {
	config     LoadTestConfig
	logger     *zap.Logger
	httpClient *http.Client

	// Metrics collection
	latencies []int64 // Store as nanoseconds for better performance
	errors    []error

	// Counters for real-time metrics
	totalRequests   int64
	successRequests int64
	failedRequests  int64

	// Test timing
	startTime time.Time
	endTime   time.Time

	mu sync.Mutex
}

// NewLoadTestRunner creates a new load test runner
func NewLoadTestRunner(config LoadTestConfig, logger *zap.Logger) *LoadTestRunner {
	// Configure HTTP client for performance testing
	transport := &http.Transport{
		MaxIdleConns:        config.Connections * 2,
		MaxConnsPerHost:     config.Connections,
		MaxIdleConnsPerHost: config.Connections,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false, // Keep connections alive for realistic testing
	}

	return &LoadTestRunner{
		config: config,
		logger: logger,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		latencies: make([]int64, 0, config.QPS*int(config.Duration.Seconds())), // Pre-allocate
	}
}

func (r *LoadTestRunner) RunLoadTest(ctx context.Context) (*LoadTestResult, error) {
	ginkgo.By(fmt.Sprintf("Starting load test: %s (QPS: %d, Connections: %d, Duration: %v)",
		r.config.TestName, r.config.QPS, r.config.Connections, r.config.Duration))

	r.logger.Info("Load test starting",
		zap.String("test_name", r.config.TestName),
		zap.Int("target_qps", r.config.QPS),
		zap.Int("connections", r.config.Connections),
		zap.Duration("duration", r.config.Duration),
		zap.String("target_url", r.config.TargetURL))

	r.startTime = time.Now()

	// Create context with timeout
	testCtx, cancel := context.WithTimeout(ctx, r.config.Duration+10*time.Second)
	defer cancel()

	// Execute load test based on QPS setting
	if r.config.QPS > 0 {
		return r.executeRateLimitedTest(testCtx)
	} else {
		// Unlimited load test (max throughput)
		return r.executeUnlimitedTest(testCtx)
	}
}

// executeRateLimitedTest runs a QPS-controlled load test
func (r *LoadTestRunner) executeRateLimitedTest(ctx context.Context) (*LoadTestResult, error) {
	// Calculate interval between requests
	interval := time.Second / time.Duration(r.config.QPS)

	// Create worker pool
	workers := make(chan struct{}, r.config.Connections)
	var wg sync.WaitGroup

	// Create ticker for QPS control
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Test duration control
	testDuration := time.NewTimer(r.config.Duration)
	defer testDuration.Stop()

	ginkgo.By(fmt.Sprintf("Running rate-limited test at %d QPS for %v", r.config.QPS, r.config.Duration))

	for {
		select {
		case <-testDuration.C:
			// Test duration completed
			wg.Wait() // Wait for all in-flight requests
			r.endTime = time.Now()
			return r.calculateResults(), nil

		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()

		case <-ticker.C:
			// Send next request
			select {
			case workers <- struct{}{}:
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-workers }()
					r.executeRequest(ctx)
				}()
			default:
				// All workers busy - this indicates we can't maintain target QPS
				atomic.AddInt64(&r.failedRequests, 1)
				r.recordError(fmt.Errorf("worker pool exhausted - cannot maintain target QPS"))
			}
		}
	}
}

// executeUnlimitedTest runs maximum throughput test
func (r *LoadTestRunner) executeUnlimitedTest(ctx context.Context) (*LoadTestResult, error) {
	var wg sync.WaitGroup

	// Test duration control
	testDuration := time.NewTimer(r.config.Duration)
	defer testDuration.Stop()

	ginkgo.By(fmt.Sprintf("Running unlimited throughput test with %d connections for %v", r.config.Connections, r.config.Duration))

	// Start workers
	for i := 0; i < r.config.Connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.continuousRequestWorker(ctx, testDuration.C)
		}()
	}

	// Wait for test completion
	<-testDuration.C
	r.endTime = time.Now()

	// Wait for all workers to finish their current requests
	wg.Wait()

	return r.calculateResults(), nil
}

// continuousRequestWorker sends requests continuously until stopped
func (r *LoadTestRunner) continuousRequestWorker(ctx context.Context, stopChan <-chan time.Time) {
	for {
		select {
		case <-stopChan:
			return
		case <-ctx.Done():
			return
		default:
			r.executeRequest(ctx)
		}
	}
}

// executeRequest performs a single HTTP request and records metrics
func (r *LoadTestRunner) executeRequest(ctx context.Context) {
	atomic.AddInt64(&r.totalRequests, 1)

	startTime := time.Now()

	// Create request
	req, err := http.NewRequestWithContext(ctx, r.config.Method, r.config.TargetURL, nil)
	if err != nil {
		atomic.AddInt64(&r.failedRequests, 1)
		r.recordError(fmt.Errorf("failed to create request: %w", err))
		return
	}

	// Add headers and handle Host header specially
	for key, value := range r.config.Headers {
		if key == "Host" {
			req.Host = value
		} else {
			req.Header.Set(key, value)
		}
	}

	// Execute request
	resp, err := r.httpClient.Do(req)
	latency := time.Since(startTime)

	// Record latency
	r.recordLatency(latency)

	if err != nil {
		atomic.AddInt64(&r.failedRequests, 1)
		r.recordError(fmt.Errorf("request failed: %w", err))
		return
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 400 {
		atomic.AddInt64(&r.failedRequests, 1)
		r.recordError(fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status))
		return
	}

	atomic.AddInt64(&r.successRequests, 1)
}

// recordLatency stores latency measurement efficiently
func (r *LoadTestRunner) recordLatency(latency time.Duration) {
	r.mu.Lock()
	r.latencies = append(r.latencies, latency.Nanoseconds())
	r.mu.Unlock()
}

// recordError stores error information
func (r *LoadTestRunner) recordError(err error) {
	r.mu.Lock()
	r.errors = append(r.errors, err)
	r.mu.Unlock()
}

// calculateResults computes final test results
func (r *LoadTestRunner) calculateResults() *LoadTestResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	totalDuration := r.endTime.Sub(r.startTime)
	totalReqs := atomic.LoadInt64(&r.totalRequests)
	successReqs := atomic.LoadInt64(&r.successRequests)
	failedReqs := atomic.LoadInt64(&r.failedRequests)

	result := &LoadTestResult{
		TotalRequests:      totalReqs,
		SuccessfulRequests: successReqs,
		FailedRequests:     failedReqs,
		Duration:           totalDuration,
		TestName:           r.config.TestName,
	}

	// Calculate rates
	if totalDuration.Seconds() > 0 {
		result.ActualQPS = float64(totalReqs) / totalDuration.Seconds()
	}

	if totalReqs > 0 {
		result.ErrorRate = float64(failedReqs) / float64(totalReqs)
	}

	// Calculate latency percentiles
	if len(r.latencies) > 0 {
		r.calculateLatencyPercentiles(result)
	}

	// Log results
	r.logger.Info("Load test completed",
		zap.String("test_name", result.TestName),
		zap.Int64("total_requests", result.TotalRequests),
		zap.Float64("actual_qps", result.ActualQPS),
		zap.Float64("error_rate", result.ErrorRate*100), // Show as percentage
		zap.Duration("p99_latency", result.LatencyP99),
		zap.Duration("p95_latency", result.LatencyP95),
		zap.Duration("mean_latency", result.LatencyMean))

	return result
}

// calculateLatencyPercentiles computes latency distribution
func (r *LoadTestRunner) calculateLatencyPercentiles(result *LoadTestResult) {
	// Convert to time.Duration slice and sort
	latencies := make([]time.Duration, len(r.latencies))
	for i, nanos := range r.latencies {
		latencies[i] = time.Duration(nanos)
	}

	// Simple insertion sort for better performance with small datasets
	for i := 1; i < len(latencies); i++ {
		key := latencies[i]
		j := i - 1
		for j >= 0 && latencies[j] > key {
			latencies[j+1] = latencies[j]
			j--
		}
		latencies[j+1] = key
	}

	n := len(latencies)

	// Calculate percentiles
	result.LatencyP50 = latencies[n*50/100]
	result.LatencyP95 = latencies[n*95/100]
	result.LatencyP99 = latencies[n*99/100]
	result.LatencyP999 = latencies[n*999/1000]
	result.LatencyMin = latencies[0]
	result.LatencyMax = latencies[n-1]

	// Calculate mean
	var sum time.Duration
	for _, lat := range latencies {
		sum += lat
	}
	result.LatencyMean = sum / time.Duration(n)
}

// ValidateResults checks if the load test results meet the specified thresholds
func (r *LoadTestRunner) ValidateResults(result *LoadTestResult) error {
	var errors []string

	if result.ErrorRate > r.config.MaxErrorRate {
		errors = append(errors, fmt.Sprintf("Error rate %.2f%% exceeds threshold %.2f%%",
			result.ErrorRate*100, r.config.MaxErrorRate*100))
	}

	if result.LatencyP99 > r.config.MaxP99Latency {
		errors = append(errors, fmt.Sprintf("P99 latency %v exceeds threshold %v",
			result.LatencyP99, r.config.MaxP99Latency))
	}

	if result.ActualQPS < r.config.MinThroughput {
		errors = append(errors, fmt.Sprintf("Throughput %.2f RPS below threshold %.2f RPS",
			result.ActualQPS, r.config.MinThroughput))
	}

	if len(errors) > 0 {
		return fmt.Errorf("load test validation failed: %v", errors)
	}

	return nil
}

// BasicLoadTest provides a simple interface for common load testing scenarios
func BasicLoadTest(ctx context.Context, targetURL string, qps int, duration time.Duration, logger *zap.Logger) (*LoadTestResult, error) {
	config := LoadTestConfig{
		TargetURL:     targetURL,
		Method:        "GET",
		QPS:           qps,
		Connections:   50, // Reasonable default
		Duration:      duration,
		MaxErrorRate:  0.01, // 1%
		MaxP99Latency: 1 * time.Second,
		MinThroughput: float64(qps) * 0.9, // 90% of target
		TestName:      fmt.Sprintf("Basic Load Test - %d QPS", qps),
	}

	runner := NewLoadTestRunner(config, logger)
	result, err := runner.RunLoadTest(ctx)
	if err != nil {
		return nil, err
	}

	// Validate results
	if err := runner.ValidateResults(result); err != nil {
		logger.Warn("Load test validation failed", zap.Error(err))
		return result, err
	}

	return result, nil
}
