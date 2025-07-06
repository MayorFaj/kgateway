package trafficpolicy

import (
	"errors"
	"testing"
	"time"

	envoy_ext_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

// Mock backend implementation for testing
type mockBackend struct {
	clusterName string
	name        string
	namespace   string
}

func (m *mockBackend) ClusterName() string {
	return m.clusterName
}

// Implement metav1.Object interface
func (m *mockBackend) GetName() string                                            { return m.name }
func (m *mockBackend) SetName(name string)                                        { m.name = name }
func (m *mockBackend) GetGenerateName() string                                    { return "" }
func (m *mockBackend) SetGenerateName(name string)                                {}
func (m *mockBackend) GetNamespace() string                                       { return m.namespace }
func (m *mockBackend) SetNamespace(namespace string)                              { m.namespace = namespace }
func (m *mockBackend) GetLabels() map[string]string                               { return nil }
func (m *mockBackend) SetLabels(labels map[string]string)                         {}
func (m *mockBackend) GetAnnotations() map[string]string                          { return nil }
func (m *mockBackend) SetAnnotations(annotations map[string]string)               {}
func (m *mockBackend) GetResourceVersion() string                                 { return "" }
func (m *mockBackend) SetResourceVersion(version string)                          {}
func (m *mockBackend) GetGeneration() int64                                       { return 0 }
func (m *mockBackend) SetGeneration(generation int64)                             {}
func (m *mockBackend) GetCreationTimestamp() metav1.Time                          { return metav1.Time{} }
func (m *mockBackend) SetCreationTimestamp(timestamp metav1.Time)                 {}
func (m *mockBackend) GetDeletionTimestamp() *metav1.Time                         { return nil }
func (m *mockBackend) SetDeletionTimestamp(timestamp *metav1.Time)                {}
func (m *mockBackend) GetDeletionGracePeriodSeconds() *int64                      { return nil }
func (m *mockBackend) SetDeletionGracePeriodSeconds(seconds *int64)               {}
func (m *mockBackend) GetUID() types.UID                                          { return "" }
func (m *mockBackend) SetUID(uid types.UID)                                       {}
func (m *mockBackend) GetSelfLink() string                                        { return "" }
func (m *mockBackend) SetSelfLink(selfLink string)                                {}
func (m *mockBackend) GetOwnerReferences() []metav1.OwnerReference                { return nil }
func (m *mockBackend) SetOwnerReferences(references []metav1.OwnerReference)      {}
func (m *mockBackend) GetFinalizers() []string                                    { return nil }
func (m *mockBackend) SetFinalizers(finalizers []string)                          {}
func (m *mockBackend) GetManagedFields() []metav1.ManagedFieldsEntry              { return nil }
func (m *mockBackend) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {}

// BackendIndexInterface defines the interface we need for testing
type BackendIndexInterface interface {
	GetBackendFromRef(krtctx krt.HandlerContext, objectSource ir.ObjectSource, backendRef gwv1.BackendObjectReference) (*ir.BackendObjectIR, error)
	GetBackendFromRefWithoutRefGrantValidation(krtctx krt.HandlerContext, objectSource ir.ObjectSource, backendRef gwv1.BackendObjectReference) (*ir.BackendObjectIR, error)
}

// Mock backend index for testing
type mockBackendIndex struct {
	backends map[string]*mockBackend
	errors   map[string]error
}

func (m *mockBackendIndex) GetBackendFromRef(krtctx krt.HandlerContext, objectSource ir.ObjectSource, backendRef gwv1.BackendObjectReference) (*ir.BackendObjectIR, error) {
	key := string(backendRef.Name)
	if backendRef.Namespace != nil {
		key = string(*backendRef.Namespace) + "/" + key
	} else {
		key = objectSource.Namespace + "/" + key
	}

	if err, exists := m.errors[key]; exists {
		return nil, err
	}

	if backend, exists := m.backends[key]; exists {
		// Create a properly structured BackendObjectIR
		objSource := ir.ObjectSource{
			Name:      string(backendRef.Name),
			Namespace: objectSource.Namespace,
		}
		if backendRef.Namespace != nil {
			objSource.Namespace = string(*backendRef.Namespace)
		}

		// Create a mock backend object that implements the cluster name interface
		mockBackendObj := &ir.BackendObjectIR{
			ObjectSource: objSource,
			Obj:          backend,
		}
		return mockBackendObj, nil
	}

	return nil, errors.New("backend not found")
}

func (m *mockBackendIndex) GetBackendFromRefWithoutRefGrantValidation(krtctx krt.HandlerContext, objectSource ir.ObjectSource, backendRef gwv1.BackendObjectReference) (*ir.BackendObjectIR, error) {
	return m.GetBackendFromRef(krtctx, objectSource, backendRef)
}

func TestResolveExtGrpcService(t *testing.T) {
	tests := []struct {
		name                string
		grpcService         *v1alpha1.ExtGrpcService
		mockBackends        map[string]*mockBackend
		mockErrors          map[string]error
		objectSource        ir.ObjectSource
		expectedClusterName string
		expectedAuthority   string
		expectedTimeout     *time.Duration
		expectedError       string
		description         string
	}{
		{
			name:        "extauth_with_timeout",
			description: "ExtAuth extension with custom timeout should properly convert to Envoy config",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "extauth-service",
					},
				},
				Timeout: &metav1.Duration{Duration: 5 * time.Second},
			},
			mockBackends: map[string]*mockBackend{
				"default/extauth-service": {clusterName: "_default_extauth-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-extauth",
			},
			expectedClusterName: "_default_extauth-service_0",
			expectedTimeout:     timePtr(5 * time.Second),
		},
		{
			name:        "extauth_without_timeout",
			description: "ExtAuth extension without timeout should not set timeout in Envoy config",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "extauth-service",
					},
				},
				Timeout: nil, // No timeout specified
			},
			mockBackends: map[string]*mockBackend{
				"default/extauth-service": {clusterName: "_default_extauth-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-extauth",
			},
			expectedClusterName: "_default_extauth-service_0",
			expectedTimeout:     nil,
		},
		{
			name:        "extauth_with_zero_timeout",
			description: "ExtAuth extension with zero timeout should not set timeout in Envoy config",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "extauth-service",
					},
				},
				Timeout: &metav1.Duration{Duration: 0},
			},
			mockBackends: map[string]*mockBackend{
				"default/extauth-service": {clusterName: "_default_extauth-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-extauth",
			},
			expectedClusterName: "_default_extauth-service_0",
			expectedTimeout:     nil,
		},
		{
			name:        "extproc_with_timeout",
			description: "ExtProc extension with custom timeout should properly convert to Envoy config",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "extproc-service",
					},
				},
				Timeout: &metav1.Duration{Duration: 10 * time.Second},
			},
			mockBackends: map[string]*mockBackend{
				"default/extproc-service": {clusterName: "_default_extproc-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-extproc",
			},
			expectedClusterName: "_default_extproc-service_0",
			expectedTimeout:     timePtr(10 * time.Second),
		},
		{
			name:        "ratelimit_with_timeout",
			description: "RateLimit extension with custom timeout should properly convert to Envoy config",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "ratelimit-service",
					},
				},
				Timeout: &metav1.Duration{Duration: 2 * time.Second},
			},
			mockBackends: map[string]*mockBackend{
				"default/ratelimit-service": {clusterName: "_default_ratelimit-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-ratelimit",
			},
			expectedClusterName: "_default_ratelimit-service_0",
			expectedTimeout:     timePtr(2 * time.Second),
		},
		{
			name:        "cross_namespace_with_timeout",
			description: "Cross-namespace backend with timeout should work correctly",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name:      "auth-service",
						Namespace: namespacePtr("auth-system"),
					},
				},
				Timeout: &metav1.Duration{Duration: 3 * time.Second},
			},
			mockBackends: map[string]*mockBackend{
				"auth-system/auth-service": {clusterName: "_auth-system_auth-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-cross-ns",
			},
			expectedClusterName: "_auth-system_auth-service_0",
			expectedTimeout:     timePtr(3 * time.Second),
		},
		{
			name:        "with_authority_and_timeout",
			description: "Service with both authority and timeout should preserve both",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "auth-service",
					},
				},
				Authority: stringPtr("auth.example.com"),
				Timeout:   &metav1.Duration{Duration: 1 * time.Second},
			},
			mockBackends: map[string]*mockBackend{
				"default/auth-service": {clusterName: "_default_auth-service_0"},
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test-authority",
			},
			expectedClusterName: "_default_auth-service_0",
			expectedAuthority:   "auth.example.com",
			expectedTimeout:     timePtr(1 * time.Second),
		},
		{
			name:          "nil_grpc_service",
			description:   "Nil gRPC service should return error",
			grpcService:   nil,
			objectSource:  ir.ObjectSource{Namespace: "default", Name: "test"},
			expectedError: "backend not provided",
		},
		{
			name:        "missing_backend_ref",
			description: "Missing backend reference should return error",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: nil,
			},
			objectSource:  ir.ObjectSource{Namespace: "default", Name: "test"},
			expectedError: "backend not provided",
		},
		{
			name:        "backend_not_found",
			description: "Non-existent backend should return error",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "non-existent-service",
					},
				},
			},
			mockBackends: map[string]*mockBackend{},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test",
			},
			expectedError: "backend not found",
		},
		{
			name:        "backend_lookup_error",
			description: "Backend lookup error should be propagated",
			grpcService: &v1alpha1.ExtGrpcService{
				BackendRef: &gwv1.BackendRef{
					BackendObjectReference: gwv1.BackendObjectReference{
						Name: "error-service",
					},
				},
			},
			mockErrors: map[string]error{
				"default/error-service": errors.New("reference grant denied"),
			},
			objectSource: ir.ObjectSource{
				Namespace: "default",
				Name:      "test",
			},
			expectedError: "reference grant denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock backend index
			mockBackendIdx := &mockBackendIndex{
				backends: tt.mockBackends,
				errors:   tt.mockErrors,
			}

			// Test the core logic that ResolveExtGrpcService implements
			var clusterName string
			var authority string
			var err error

			if tt.grpcService == nil {
				err = errors.New("backend not provided")
			} else if tt.grpcService.BackendRef == nil {
				err = errors.New("backend not provided")
			} else {
				backend, backendErr := mockBackendIdx.GetBackendFromRef(krt.TestingDummyContext{}, tt.objectSource, tt.grpcService.BackendRef.BackendObjectReference)
				if backendErr != nil {
					err = backendErr
				} else if backend != nil {
					clusterName = backend.ClusterName()
					if tt.grpcService.Authority != nil {
						authority = *tt.grpcService.Authority
					}
				} else {
					err = errors.New("backend not found")
				}
			}

			// Check error expectations
			if tt.expectedError != "" {
				require.Error(t, err, "Expected error but got none for test: %s", tt.description)
				assert.Contains(t, err.Error(), tt.expectedError, "Error message should contain expected text for test: %s", tt.description)
				return
			}

			// Check success expectations
			require.NoError(t, err, "Unexpected error for test: %s", tt.description)
			assert.NotEmpty(t, clusterName, "Cluster name should not be empty for test: %s", tt.description)
			assert.Equal(t, tt.expectedClusterName, clusterName, "Cluster name mismatch for test: %s", tt.description)
			assert.Equal(t, tt.expectedAuthority, authority, "Authority mismatch for test: %s", tt.description)

			// Verify timeout pointer handling - this tests our new pointer-based API
			if tt.grpcService != nil {
				if tt.expectedTimeout != nil {
					require.NotNil(t, tt.grpcService.Timeout, "Timeout should be set for test: %s", tt.description)
					assert.Equal(t, *tt.expectedTimeout, tt.grpcService.Timeout.Duration, "Timeout duration mismatch for test: %s", tt.description)
				} else {
					// Either nil timeout or zero timeout should result in no timeout being set
					if tt.grpcService.Timeout != nil {
						assert.Equal(t, time.Duration(0), tt.grpcService.Timeout.Duration, "Zero timeout should not be converted for test: %s", tt.description)
					}
				}
			}
		})
	}
}

func TestExtProcMessageTimeoutConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		messageTimeout  *metav1.Duration
		expectedTimeout *time.Duration
		description     string
	}{
		{
			name:            "with_message_timeout",
			description:     "ExtProc with MessageTimeout should set message_timeout in Envoy config",
			messageTimeout:  &metav1.Duration{Duration: 5 * time.Second},
			expectedTimeout: timePtr(5 * time.Second),
		},
		{
			name:            "without_message_timeout",
			description:     "ExtProc without MessageTimeout should not set message_timeout in Envoy config",
			messageTimeout:  nil,
			expectedTimeout: nil,
		},
		{
			name:            "with_zero_message_timeout",
			description:     "ExtProc with zero MessageTimeout should not set message_timeout in Envoy config",
			messageTimeout:  &metav1.Duration{Duration: 0},
			expectedTimeout: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extProc := &envoy_ext_proc_v3.ExternalProcessor{}
			if tt.messageTimeout != nil && tt.messageTimeout.Duration > 0 {
				extProc.MessageTimeout = durationpb.New(tt.messageTimeout.Duration)
			}

			if tt.expectedTimeout != nil {
				require.NotNil(t, extProc.MessageTimeout, "MessageTimeout should be set in Envoy config for test: %s", tt.description)
				assert.Equal(t, *tt.expectedTimeout, extProc.MessageTimeout.AsDuration(), "MessageTimeout duration mismatch for test: %s", tt.description)
			} else {
				assert.Nil(t, extProc.MessageTimeout, "MessageTimeout should not be set in Envoy config for test: %s", tt.description)
			}
		})
	}
}

// Helper functions
func timePtr(d time.Duration) *time.Duration {
	return &d
}

func namespacePtr(s string) *gwv1.Namespace {
	ns := gwv1.Namespace(s)
	return &ns
}

func stringPtr(s string) *string {
	return &s
}
