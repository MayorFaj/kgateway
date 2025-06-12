package performance

import (
	"path/filepath"
	"time"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/fsutils"
)

var (
	// Manifest file paths
	performanceManifest = filepath.Join(fsutils.MustGetThisDir(), "testdata", "manifests.yaml")
)

// LoadTestConfig defines parameters for in-cluster load testing
type LoadTestConfig struct {
	TestName    string
	ServiceFQDN string
	Path        string
	Headers     map[string]string

	// Load parameters
	RequestCount    int           // Total number of requests to send
	ConcurrentJobs  int           // Number of concurrent curl jobs
	RequestInterval time.Duration // Interval between requests

	// Validation thresholds
	MaxErrorRate    float64 // Maximum acceptable error rate (0.01 = 1%)
	MinSuccessRate  float64 // Minimum success rate required
	MaxFailureCount int     // Maximum number of failures allowed
}

// LoadTestResult contains the results of an in-cluster load test
type LoadTestResult struct {
	TestName        string
	TotalRequests   int
	SuccessRequests int
	FailedRequests  int
	ErrorRate       float64
	SuccessRate     float64
	Duration        time.Duration

	// Sample response codes
	ResponseCodes map[int]int

	// Error details
	SampleErrors []string
}
