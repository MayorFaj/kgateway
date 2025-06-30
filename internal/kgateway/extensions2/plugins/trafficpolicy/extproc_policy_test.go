package trafficpolicy

import (
	"testing"
	"time"

	envoy_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

func TestBuildEnvoyExtProc(t *testing.T) {
	tests := []struct {
		name           string
		gatewayExt     *ir.GatewayExtension
		extprocConfig  *v1alpha1.ExtProcPolicy
		expectedError  string
		validateResult func(*testing.T, *envoy_ext_proc_v3.ExtProcPerRoute)
	}{
		{
			name: "with all processing modes",
			gatewayExt: &ir.GatewayExtension{
				ExtProc: &v1alpha1.ExtProcProvider{
					GrpcService: &v1alpha1.ExtGrpcService{},
				},
			},
			extprocConfig: &v1alpha1.ExtProcPolicy{
				ProcessingMode: &v1alpha1.ProcessingMode{
					RequestHeaderMode:   ptr.To("SEND"),
					ResponseHeaderMode:  ptr.To("SKIP"),
					RequestBodyMode:     ptr.To("STREAMED"),
					ResponseBodyMode:    ptr.To("BUFFERED"),
					RequestTrailerMode:  ptr.To("SEND"),
					ResponseTrailerMode: ptr.To("SKIP"),
				},
			},
			validateResult: func(t *testing.T, result *envoy_ext_proc_v3.ExtProcPerRoute) {
				processingMode := result.GetOverrides().GetProcessingMode()
				assert.NotNil(t, processingMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_SEND, processingMode.RequestHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_SKIP, processingMode.ResponseHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_STREAMED, processingMode.RequestBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_BUFFERED, processingMode.ResponseBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_SEND, processingMode.RequestTrailerMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_SKIP, processingMode.ResponseTrailerMode)
			},
		},
		{
			name: "with default processing modes",
			gatewayExt: &ir.GatewayExtension{
				ExtProc: &v1alpha1.ExtProcProvider{
					GrpcService: &v1alpha1.ExtGrpcService{},
				},
			},
			extprocConfig: &v1alpha1.ExtProcPolicy{
				ProcessingMode: &v1alpha1.ProcessingMode{},
			},
			validateResult: func(t *testing.T, result *envoy_ext_proc_v3.ExtProcPerRoute) {
				processingMode := result.GetOverrides().GetProcessingMode()
				assert.NotNil(t, processingMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.RequestHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.ResponseHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_NONE, processingMode.RequestBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_NONE, processingMode.ResponseBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.RequestTrailerMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.ResponseTrailerMode)
			},
		},
		{
			name: "with invalid processing modes",
			gatewayExt: &ir.GatewayExtension{
				ExtProc: &v1alpha1.ExtProcProvider{
					GrpcService: &v1alpha1.ExtGrpcService{},
				},
			},
			extprocConfig: &v1alpha1.ExtProcPolicy{
				ProcessingMode: &v1alpha1.ProcessingMode{
					RequestHeaderMode:   ptr.To("INVALID"),
					ResponseHeaderMode:  ptr.To("INVALID"),
					RequestBodyMode:     ptr.To("INVALID"),
					ResponseBodyMode:    ptr.To("INVALID"),
					RequestTrailerMode:  ptr.To("INVALID"),
					ResponseTrailerMode: ptr.To("INVALID"),
				},
			},
			validateResult: func(t *testing.T, result *envoy_ext_proc_v3.ExtProcPerRoute) {
				processingMode := result.GetOverrides().GetProcessingMode()
				assert.NotNil(t, processingMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.RequestHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.ResponseHeaderMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_NONE, processingMode.RequestBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_NONE, processingMode.ResponseBodyMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.RequestTrailerMode)
				assert.Equal(t, envoy_ext_proc_v3.ProcessingMode_DEFAULT, processingMode.ResponseTrailerMode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translateExtProcPerFilterConfig(tt.extprocConfig)

			//require.NoError(t, err)
			require.NotNil(t, result)
			tt.validateResult(t, result)
		})
	}
}

func TestExtProcTimeouts(t *testing.T) {
	t.Run("configures timeout when specified", func(t *testing.T) {
		// Setup
		extension := &ir.GatewayExtension{
			ExtProc: &v1alpha1.ExtProcProvider{
				GrpcService: &v1alpha1.ExtGrpcService{
					BackendRef: &gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Name: "test-service",
						},
					},
					Timeout: metav1.Duration{Duration: 10 * time.Second},
				},
			},
		}

		// Verify
		assert.Equal(t, 10*time.Second, extension.ExtProc.GrpcService.Timeout.Duration)
	})

	t.Run("uses default timeout when not specified", func(t *testing.T) {
		// Setup
		extension := &ir.GatewayExtension{
			ExtProc: &v1alpha1.ExtProcProvider{
				GrpcService: &v1alpha1.ExtGrpcService{
					BackendRef: &gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Name: "test-service",
						},
					},
					// No timeout specified
				},
			},
		}

		// Verify
		assert.Equal(t, time.Duration(0), extension.ExtProc.GrpcService.Timeout.Duration)
	})

	t.Run("configures messageTimeout when specified", func(t *testing.T) {
		// Setup
		extension := &ir.GatewayExtension{
			ExtProc: &v1alpha1.ExtProcProvider{
				GrpcService: &v1alpha1.ExtGrpcService{
					BackendRef: &gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Name: "test-service",
						},
					},
				},
				MessageTimeout: metav1.Duration{Duration: 200 * time.Millisecond},
			},
		}

		// Verify
		assert.Equal(t, 200*time.Millisecond, extension.ExtProc.MessageTimeout.Duration)
	})

	t.Run("uses default messageTimeout when not specified", func(t *testing.T) {
		// Setup
		extension := &ir.GatewayExtension{
			ExtProc: &v1alpha1.ExtProcProvider{
				GrpcService: &v1alpha1.ExtGrpcService{
					BackendRef: &gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Name: "test-service",
						},
					},
				},
				// No messageTimeout specified
			},
		}

		// Verify
		assert.Equal(t, time.Duration(0), extension.ExtProc.MessageTimeout.Duration)
	})

	t.Run("configures both timeout and messageTimeout", func(t *testing.T) {
		// Setup
		extension := &ir.GatewayExtension{
			ExtProc: &v1alpha1.ExtProcProvider{
				GrpcService: &v1alpha1.ExtGrpcService{
					BackendRef: &gwv1.BackendRef{
						BackendObjectReference: gwv1.BackendObjectReference{
							Name: "test-service",
						},
					},
					Timeout: metav1.Duration{Duration: 5 * time.Second},
				},
				MessageTimeout: metav1.Duration{Duration: 100 * time.Millisecond},
			},
		}

		// Verify both timeouts are configured independently
		assert.Equal(t, 5*time.Second, extension.ExtProc.GrpcService.Timeout.Duration)
		assert.Equal(t, 100*time.Millisecond, extension.ExtProc.MessageTimeout.Duration)
	})
}
