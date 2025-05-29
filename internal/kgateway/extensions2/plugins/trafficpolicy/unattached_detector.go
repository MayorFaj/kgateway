package trafficpolicy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
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
			exists, err := d.targetRefExists(ctx, targetRef.LocalPolicyTargetReference, policy.Namespace)
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

		// Check each target selector
		for _, targetSelector := range policy.Spec.TargetSelectors {
			hasMatchingTargets, err := d.targetSelectorHasMatches(ctx, targetSelector, policy.Namespace)
			if err != nil {
				logger.Warn("error checking target selector matches",
					"policy", policy.Name,
					"namespace", policy.Namespace,
					"targetSelector", targetSelector,
					"error", err)
				continue
			}
			if !hasMatchingTargets {
				hasUnattachedTargets = true
				logger.Debug("found target selector with no matches",
					"policy", policy.Name,
					"namespace", policy.Namespace,
					"targetSelector", targetSelector)
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
func (d *TrafficPolicyUnattachedDetector) targetRefExists(ctx context.Context, targetRef v1alpha1.LocalPolicyTargetReference, namespace string) (bool, error) {
	// Create the NamespacedName for the target
	targetKey := types.NamespacedName{
		Name:      string(targetRef.Name),
		Namespace: namespace,
	}

	// Check based on the target kind
	switch string(targetRef.Kind) {
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

	case wellknown.BackendGVK.Kind:
		var backend v1alpha1.Backend
		err := d.client.Get(ctx, targetKey, &backend)
		return err == nil, client.IgnoreNotFound(err)

	default:
		// For unknown kinds, use dynamic discovery to check if the resource exists
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: string(targetRef.Group),
			Kind:  string(targetRef.Kind),
		})
		err := d.client.Get(ctx, targetKey, obj)
		if err != nil {
			logger.Debug("failed to find resource with discovered gvk",
				"target", targetKey,
				"error", err)
			return false, client.IgnoreNotFound(err)
		}
		return true, nil
	}
}

// targetSelectorHasMatches checks if the target selector has any matching resources
func (d *TrafficPolicyUnattachedDetector) targetSelectorHasMatches(ctx context.Context, targetSelector v1alpha1.LocalPolicyTargetSelector, namespace string) (bool, error) {
	// Create label selector
	selector := labels.SelectorFromSet(targetSelector.MatchLabels)

	// Check based on the target kind
	switch string(targetSelector.Kind) {
	case wellknown.GatewayKind:
		var gateways gwv1.GatewayList
		err := d.client.List(ctx, &gateways,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(gateways.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.HTTPRouteKind:
		var httpRoutes gwv1.HTTPRouteList
		err := d.client.List(ctx, &httpRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(httpRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.TCPRouteKind:
		var tcpRoutes gwv1a2.TCPRouteList
		err := d.client.List(ctx, &tcpRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(tcpRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.TLSRouteKind:
		var tlsRoutes gwv1a2.TLSRouteList
		err := d.client.List(ctx, &tlsRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(tlsRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.GRPCRouteKind:
		var grpcRoutes gwv1.GRPCRouteList
		err := d.client.List(ctx, &grpcRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(grpcRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.BackendGVK.Kind:
		var backends v1alpha1.BackendList
		err := d.client.List(ctx, &backends,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(backends.Items) > 0, client.IgnoreNotFound(err)

	default:
		// For unknown kinds, use dynamic discovery to check if any resources exist
		// This provides better coverage than just assuming targets exist
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group: string(targetSelector.Group),
			Kind:  string(targetSelector.Kind),
		})
		err := d.client.List(ctx, list,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		if err != nil {
			logger.Debug("failed to list resources with discovered gvk",
				"namespace", namespace,
				"selector", targetSelector.MatchLabels,
				"error", err)
			return false, client.IgnoreNotFound(err)
		}
		return len(list.Items) > 0, nil
	}
}
