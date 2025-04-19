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

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
)

const (
	globalRateLimitFilterName       = "envoy.filters.http.ratelimit"
	globalRateLimitFilterNamePrefix = "ratelimit/global"
	globalRateLimitStatPrefix       = "http_global_rate_limit"
)

// toGlobalRateLimitFilterConfig translates a GlobalRateLimitPolicy to Envoy rate limit configuration.
func toGlobalRateLimitFilterConfig(policy *v1alpha1.GlobalRateLimitPolicy, extensionName string) (*ratev3.RateLimit, error) {
	if policy == nil {
		return nil, nil
	}

	// Create a timeout based on the time unit if specified, otherwise use a default
	timeout := durationpb.New(time.Second * 2) // Default timeout

	// Set appropriate timeout based on unit if specified
	if policy.Unit != "" {
		switch policy.Unit {
		case "second":
			timeout = durationpb.New(time.Millisecond * 500) // Shorter timeout for per-second limits
		case "minute":
			timeout = durationpb.New(time.Second * 2) // Standard timeout for per-minute limits
		case "hour", "day":
			timeout = durationpb.New(time.Second * 5) // Longer timeout for less frequent limits
		default:
			return nil, fmt.Errorf("unsupported time unit: %s", policy.Unit) // Explicitly handle unsupported units
		}
	}

	// Construct cluster name from extension name
	clusterName := fmt.Sprintf("ext.%s", extensionName)

	// Create a rate limit configuration
	rl := &ratev3.RateLimit{
		Domain:          policy.Domain,
		Timeout:         timeout,
		FailureModeDeny: !policy.FailOpen,
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
