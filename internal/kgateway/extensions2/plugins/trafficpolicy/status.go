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
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	extensionsplug "github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugin"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
)

func getPolicyStatusFn(
	cl client.Client,
) extensionsplug.GetPolicyStatusFn {
	return func(ctx context.Context, nn types.NamespacedName) (gwv1alpha2.PolicyStatus, error) {
		res := v1alpha1.TrafficPolicy{}
		err := cl.Get(ctx, nn, &res)
		if err != nil {
			return gwv1alpha2.PolicyStatus{}, err
		}
		return res.Status, nil
	}
}

func patchPolicyStatusFn(
	cl client.Client,
) extensionsplug.PatchPolicyStatusFn {
	return func(ctx context.Context, nn types.NamespacedName, policyStatus gwv1alpha2.PolicyStatus) error {
		res := v1alpha1.TrafficPolicy{}
		err := cl.Get(ctx, nn, &res)
		if err != nil {
			return err
		}

		res.Status = policyStatus
		if err := cl.Status().Patch(ctx, &res, client.Merge); err != nil {
			return fmt.Errorf("error updating status for TrafficPolicy %s: %w", nn.String(), err)
		}
		return nil
	}
}

func getUnattachedPoliciesFn(
	cl client.Client,
) extensionsplug.GetUnattachedPoliciesFn {
	return func(ctx context.Context, gk schema.GroupKind) ([]types.NamespacedName, error) {
		var unattachedPolicies []types.NamespacedName

		var trafficPolicies v1alpha1.TrafficPolicyList
		if err := cl.List(ctx, &trafficPolicies); err != nil {
			return nil, fmt.Errorf("failed to list TrafficPolicy resources: %w", err)
		}

		for _, policy := range trafficPolicies.Items {
			hasUnattachedTargets := false

			for _, targetRef := range policy.Spec.TargetRefs {
				exists, err := targetRefExists(ctx, cl, targetRef.LocalPolicyTargetReference, policy.Namespace)
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
				hasMatchingTargets, err := targetSelectorHasMatches(ctx, cl, targetSelector, policy.Namespace)
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
}

// targetRefExists checks if the target resource referenced by the given TargetRef exists
func targetRefExists(ctx context.Context, cl client.Client, targetRef v1alpha1.LocalPolicyTargetReference, namespace string) (bool, error) {
	targetKey := types.NamespacedName{
		Name:      string(targetRef.Name),
		Namespace: namespace,
	}

	switch string(targetRef.Kind) {
	case wellknown.GatewayKind:
		var gateway gwv1.Gateway
		err := cl.Get(ctx, targetKey, &gateway)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.HTTPRouteKind:
		var httpRoute gwv1.HTTPRoute
		err := cl.Get(ctx, targetKey, &httpRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.TCPRouteKind:
		var tcpRoute gwv1alpha2.TCPRoute
		err := cl.Get(ctx, targetKey, &tcpRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.TLSRouteKind:
		var tlsRoute gwv1alpha2.TLSRoute
		err := cl.Get(ctx, targetKey, &tlsRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.GRPCRouteKind:
		var grpcRoute gwv1.GRPCRoute
		err := cl.Get(ctx, targetKey, &grpcRoute)
		return err == nil, client.IgnoreNotFound(err)

	case wellknown.BackendGVK.Kind:
		var backend v1alpha1.Backend
		err := cl.Get(ctx, targetKey, &backend)
		return err == nil, client.IgnoreNotFound(err)

	default:
		// For unknown kinds, use dynamic discovery to check if the resource exists
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: string(targetRef.Group),
			Kind:  string(targetRef.Kind),
		})
		err := cl.Get(ctx, targetKey, obj)
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
func targetSelectorHasMatches(ctx context.Context, cl client.Client, targetSelector v1alpha1.LocalPolicyTargetSelector, namespace string) (bool, error) {
	selector := labels.SelectorFromSet(targetSelector.MatchLabels)

	switch string(targetSelector.Kind) {
	case wellknown.GatewayKind:
		var gateways gwv1.GatewayList
		err := cl.List(ctx, &gateways,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(gateways.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.HTTPRouteKind:
		var httpRoutes gwv1.HTTPRouteList
		err := cl.List(ctx, &httpRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(httpRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.TCPRouteKind:
		var tcpRoutes gwv1alpha2.TCPRouteList
		err := cl.List(ctx, &tcpRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(tcpRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.TLSRouteKind:
		var tlsRoutes gwv1alpha2.TLSRouteList
		err := cl.List(ctx, &tlsRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(tlsRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.GRPCRouteKind:
		var grpcRoutes gwv1.GRPCRouteList
		err := cl.List(ctx, &grpcRoutes,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(grpcRoutes.Items) > 0, client.IgnoreNotFound(err)

	case wellknown.BackendGVK.Kind:
		var backends v1alpha1.BackendList
		err := cl.List(ctx, &backends,
			client.InNamespace(namespace),
			client.MatchingLabelsSelector{Selector: selector})
		return len(backends.Items) > 0, client.IgnoreNotFound(err)

	default:
		// For unknown kinds, use dynamic discovery to check if any resources exist
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group: string(targetSelector.Group),
			Kind:  string(targetSelector.Kind),
		})
		err := cl.List(ctx, list,
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
