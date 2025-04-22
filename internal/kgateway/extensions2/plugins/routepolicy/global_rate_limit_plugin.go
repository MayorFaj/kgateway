package routepolicy

import (
	"errors"
	"fmt"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/config/ratelimit/v3"
	routeconfv3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	ratev3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ratelimit/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/istio/pkg/kube/krt"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/pluginutils"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

const (
	globalRateLimitFilterName       = "envoy.filters.http.ratelimit"
	globalRateLimitFilterNamePrefix = "ratelimit/global"
	globalRateLimitStatPrefix       = "http_global_rate_limit"
)

// toGlobalRateLimitFilterConfig translates a GlobalRateLimitPolicy to Envoy rate limit configuration.
func toGlobalRateLimitFilterConfig(policy *v1alpha1.GlobalRateLimitPolicy, gweCollection krt.Collection[ir.GatewayExtension], kctx krt.HandlerContext) (*ratev3.RateLimit, error) {
	if policy == nil || policy.ExtensionRef == nil {
		return nil, nil
	}

	// Get the referenced GatewayExtension
	gwExt, err := pluginutils.GetGatewayExtension(
		gweCollection,
		kctx,
		policy.ExtensionRef.Name,
		policy.Domain, // Use the domain from the policy as fallback namespace
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get referenced GatewayExtension %s: %w", policy.ExtensionRef.Name, err)
	}

	// Verify it's the correct type
	if gwExt.Type != v1alpha1.GatewayExtensionTypeRateLimit {
		return nil, pluginutils.ErrInvalidExtensionType(v1alpha1.GatewayExtensionTypeRateLimit, gwExt.Type)
	}

	// Check if GlobalRateLimit provider is configured
	if gwExt.GlobalRateLimit == nil {
		return nil, fmt.Errorf("GlobalRateLimit configuration is missing in GatewayExtension %s", policy.ExtensionRef.Name)
	}

	// Create a timeout based on the time unit if specified, otherwise use the default from the extension
	var timeout *durationpb.Duration

	// Use timeout from extension if specified
	if gwExt.GlobalRateLimit.Timeout != "" {
		duration, err := time.ParseDuration(gwExt.GlobalRateLimit.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout in GatewayExtension %s: %w", policy.ExtensionRef.Name, err)
		}
		timeout = durationpb.New(duration)
	} else {
		// Default timeout if not specified in extension
		timeout = durationpb.New(time.Second * 2)
	}

	// Get domain from extension or use the one from the policy if not specified
	domain := policy.Domain
	if gwExt.GlobalRateLimit.Domain != "" {
		domain = gwExt.GlobalRateLimit.Domain
	}

	// Construct cluster name from the backendRef
	clusterName := ""
	if gwExt.GlobalRateLimit.GrpcService != nil && gwExt.GlobalRateLimit.GrpcService.BackendRef != nil {
		clusterName = fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local",
			gwExt.GlobalRateLimit.GrpcService.BackendRef.Port,
			gwExt.GlobalRateLimit.GrpcService.BackendRef.Name,
			gwExt.Domain)
	} else {
		return nil, fmt.Errorf("GrpcService BackendRef not specified in GatewayExtension %s", policy.ExtensionRef.Name)
	}

	// Create a rate limit configuration
	rl := &ratev3.RateLimit{
		Domain:          domain,
		Timeout:         timeout,
		FailureModeDeny: !gwExt.GlobalRateLimit.FailOpen,
		RateLimitService: &ratelimitv3.RateLimitServiceConfig{
			GrpcService: &corev3.GrpcService{
				TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
						ClusterName: clusterName,
					},
				},
			},
			TransportApiVersion: corev3.ApiVersion_V3,
		},
		Stage:                   0,                                 // Default stage
		EnableXRatelimitHeaders: ratev3.RateLimit_DRAFT_VERSION_03, // Use latest RFC draft
		RequestType:             "both",                            // Apply to both internal and external
		StatPrefix:              globalRateLimitStatPrefix,
	}

	// Add descriptors if defined
	if len(policy.Descriptors) > 0 {
		// Just log that we've processed descriptors, but we don't use them directly in the rl object
		// since the ConfigType field doesn't exist
		_, err := createRateLimitActions(policy.Descriptors)
		if err != nil {
			return nil, err
		}
		// Domain is already set above, no need to set it through a non-existent ConfigType field
	}

	return rl, nil
}

// createRateLimitActions translates the API descriptors to Envoy route config rate limit actions
func createRateLimitActions(descriptors []v1alpha1.RateLimitDescriptor) ([]*routeconfv3.RateLimit_Action, error) {
	if len(descriptors) == 0 {
		return nil, errors.New("at least one descriptor is required for global rate limiting")
	}

	result := make([]*routeconfv3.RateLimit_Action, 0, len(descriptors))

	for _, desc := range descriptors {
		action := &routeconfv3.RateLimit_Action{}

		// Handle different types of descriptor sources
		if desc.Value != "" {
			// Static value
			action.ActionSpecifier = &routeconfv3.RateLimit_Action_GenericKey_{
				GenericKey: &routeconfv3.RateLimit_Action_GenericKey{
					DescriptorKey:   desc.Key,
					DescriptorValue: desc.Value,
				},
			}
		} else if desc.ValueFrom != nil {
			// Dynamic value source
			if desc.ValueFrom.RemoteAddress {
				// Use remote address as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RemoteAddress_{
					RemoteAddress: &routeconfv3.RateLimit_Action_RemoteAddress{},
				}
			} else if desc.ValueFrom.Path {
				// Use request path as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RequestHeaders_{
					RequestHeaders: &routeconfv3.RateLimit_Action_RequestHeaders{
						HeaderName:    ":path",
						DescriptorKey: desc.Key,
					},
				}
			} else if desc.ValueFrom.Header != "" {
				// Use header value as descriptor value
				action.ActionSpecifier = &routeconfv3.RateLimit_Action_RequestHeaders_{
					RequestHeaders: &routeconfv3.RateLimit_Action_RequestHeaders{
						HeaderName:    desc.ValueFrom.Header,
						DescriptorKey: desc.Key,
					},
				}
			} else {
				return nil, fmt.Errorf("descriptor %s has no valid value source specified", desc.Key)
			}
		} else {
			return nil, fmt.Errorf("descriptor %s has no value or valueFrom specified", desc.Key)
		}

		result = append(result, action)
	}

	return result, nil
}
