package trafficpolicy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
	pluginsdk "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk"
)

// TrafficPolicyUnattachedDetector implements the UnattachedPolicyDetector interface
// for TrafficPolicy resources, detecting policies with non-existent target references.
type TrafficPolicyUnattachedDetector struct {
	client client.Client
}

// NewTrafficPolicyUnattachedDetector creates a new TrafficPolicyUnattachedDetector
func NewTrafficPolicyUnattachedDetector(client client.Client) pluginsdk.UnattachedPolicyDetector {
	return &TrafficPolicyUnattachedDetector{
		client: client,
	}
}

// DetectUnattachedPolicies returns a list of TrafficPolicy resources that have
// non-existent target references for the given GroupKind.
func (d *TrafficPolicyUnattachedDetector) DetectUnattachedPolicies(ctx context.Context, gk schema.GroupKind) ([]types.NamespacedName, error) {
	// Only handle TrafficPolicy GroupKind
	if gk != wellknown.TrafficPolicyGVK.GroupKind() {
		return nil, nil
	}

	var unattachedPolicies []types.NamespacedName

	// List all TrafficPolicy resources
	var trafficPolicies v1alpha1.TrafficPolicyList
	if err := d.client.List(ctx, &trafficPolicies); err != nil {
		return nil, fmt.Errorf("failed to list TrafficPolicy resources: %w", err)
	}

	// Check each policy for unattached target references
	for _, policy := range trafficPolicies.Items {
		hasUnattachedTargets := false

		// Check each target reference
		for _, targetRef := range policy.Spec.TargetRefs {
			exists, err := d.targetExists(ctx, targetRef.LocalPolicyTargetReference, policy.Namespace)
			if err != nil {
				logger.Warn("error checking target reference existence",
					"policy", policy.Name,
					"namespace", policy.Namespace,
					"targetRef", targetRef,
					"error", err)
				continue
			}
			if !exists {
				hasUnattachedTargets = true
				logger.Debug("found unattached target reference",
					"policy", policy.Name,
					"namespace", policy.Namespace,
					"targetRef", targetRef)
			}
		}

		if hasUnattachedTargets {
			unattachedPolicies = append(unattachedPolicies, types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			})
		}
	}

	return unattachedPolicies, nil
}

// targetExists checks if the target resource referenced by the given TargetRef exists
func (d *TrafficPolicyUnattachedDetector) targetExists(ctx context.Context, targetRef v1alpha1.LocalPolicyTargetReference, namespace string) (bool, error) {
	// Create the NamespacedName for the target
	targetKey := types.NamespacedName{
		Name:      string(targetRef.Name),
		Namespace: namespace,
	}

	// Check based on the target kind
	switch targetRef.Kind {
	case wellknown.GatewayKind:
		var gateway gwv1.Gateway
		err := d.client.Get(ctx, targetKey, &gateway)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.HTTPRouteKind:
		var httpRoute gwv1.HTTPRoute
		err := d.client.Get(ctx, targetKey, &httpRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.TCPRouteKind:
		var tcpRoute gwv1a2.TCPRoute
		err := d.client.Get(ctx, targetKey, &tcpRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.TLSRouteKind:
		var tlsRoute gwv1a2.TLSRoute
		err := d.client.Get(ctx, targetKey, &tlsRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.GRPCRouteKind:
		var grpcRoute gwv1.GRPCRoute
		err := d.client.Get(ctx, targetKey, &grpcRoute)
		return err == nil, client.IgnoreNotFound(err)

	case "Backend":
		var backend v1alpha1.Backend
		err := d.client.Get(ctx, targetKey, &backend)
		return err == nil, client.IgnoreNotFound(err)

	default:
		// For unknown kinds, assume they exist to avoid false positives
		logger.Debug("unknown target kind, assuming target exists",
			"kind", targetRef.Kind,
			"target", targetKey)
		return true, nil
	}
}
