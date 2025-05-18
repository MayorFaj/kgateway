package trafficpolicy

import (
	"context"
	"fmt"

	"github.com/solo-io/go-utils/contextutils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/proxy_syncer"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/reports"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
	reportssdk "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/reporter"
)

// TrafficPolicyUnattachedHandler handles TrafficPolicy resources with non-existent targetRefs
type TrafficPolicyUnattachedHandler struct {
	client         client.Client
	controllerName string
}

// GroupKind returns the GroupKind for TrafficPolicy resources
func (h *TrafficPolicyUnattachedHandler) GroupKind() schema.GroupKind {
	return wellknown.TrafficPolicyGVK.GroupKind()
}

// NewTrafficPolicyUnattachedHandler creates a new handler for TrafficPolicy resources
func NewTrafficPolicyUnattachedHandler(client client.Client, controllerName string) proxy_syncer.UnattachedPolicyHandler {
	return &TrafficPolicyUnattachedHandler{
		client:         client,
		controllerName: controllerName,
	}
}

// HandleUnattachedPolicies checks for TrafficPolicies with non-existent targetRefs
// and creates status reports for them
func (h *TrafficPolicyUnattachedHandler) HandleUnattachedPolicies(ctx context.Context, resourcesReportMap reports.ReportMap) (reports.ReportMap, error) {
	// Get all TrafficPolicies
	var trafficPolicies v1alpha1.TrafficPolicyList
	if err := h.client.List(ctx, &trafficPolicies); err != nil {
		return resourcesReportMap, fmt.Errorf("error listing TrafficPolicies: %w", err)
	}

	// Keep track of TrafficPolicies that might not be attached to any objects
	// due to non-existent targetRefs
	unattachedReportMap := reports.NewReportMap()
	reporter := reports.NewReporter(&unattachedReportMap)

	for _, policy := range trafficPolicies.Items {
		// Skip if policy has no targetRefs
		if len(policy.Spec.TargetRefs) == 0 {
			continue
		}

		policyKey := reports.PolicyKey{
			Group:     policy.GroupVersionKind().Group,
			Kind:      policy.Kind,
			Namespace: policy.Namespace,
			Name:      policy.Name,
		}

		// Skip if policy already has status reports (meaning it's attached to at least one object)
		if resourcesReportMap.Policies[policyKey] != nil {
			continue
		}

		// For each targetRef in the policy, check if it exists
		for _, targetRef := range policy.Spec.TargetRefs {
			// Convert targetRef to ParentReference format for status
			parentRef := gwv1.ParentReference{
				Group:     ptr.To(gwv1.Group(targetRef.Group)),
				Kind:      ptr.To(gwv1.Kind(targetRef.Kind)),
				Name:      gwv1.ObjectName(targetRef.Name),
				Namespace: ptr.To(gwv1.Namespace(policy.Namespace)), // Use the policy's namespace as default for target
			}

			// Get the target GroupKind
			targetGK := schema.GroupKind{
				Group: string(targetRef.Group),
				Kind:  string(targetRef.Kind),
			}

			// Create GVK with an initial value
			var gvk schema.GroupVersionKind

			// Try to get the preferred version from the REST mapper
			mapping, err := h.client.RESTMapper().RESTMapping(targetGK)
			if err == nil {
				// Use the discovered version
				gvk = mapping.GroupVersionKind
			} else {
				// If we can't discover via REST mapper, use basic fallback logic
				gvk = schema.GroupVersionKind{
					Group:   targetGK.Group,
					Kind:    targetGK.Kind,
					Version: "v1", // Most common default version
				}

				// Log that we're falling back
				contextutils.LoggerFrom(ctx).Debugw(
					"Could not discover API version using RESTMapper, using fallback",
					"group", targetGK.Group,
					"kind", targetGK.Kind,
					"fallbackVersion", "v1",
					"error", err,
				)
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)

			// Check if the target exists in the policy's namespace
			err = h.client.Get(ctx, types.NamespacedName{
				Namespace: policy.Namespace,
				Name:      string(targetRef.Name),
			}, obj)

			// If there's an error, create a status report
			if err != nil {
				// Only generate a status report for NotFound errors
				if apierrors.IsNotFound(err) {
					// Create status report for the missing targetRef
					policyReport := reporter.Policy(policyKey, policy.Generation)
					policyReport.AncestorRef(parentRef).SetCondition(reportssdk.PolicyCondition{
						Type:    gwv1alpha2.PolicyConditionAccepted,
						Status:  metav1.ConditionFalse,
						Reason:  gwv1alpha2.PolicyConditionReason(reportssdk.PolicyReasonTargetNotFound),
						Message: reportssdk.PolicyTargetNotFoundMsg,
					})
				} else {
					// For non-NotFound errors, log them instead of putting in status
					// Users can't act on these kinds of errors directly
					contextutils.LoggerFrom(ctx).Error(
						fmt.Sprintf("Error checking existence of target for policy %s/%s: %v",
							policy.Namespace, policy.Name, err))
				}
			}
		}
	}

	// Merge the unattached policy reports with the existing reports
	mergedReportMap := reports.MergeReportMaps(resourcesReportMap, unattachedReportMap)
	return mergedReportMap, nil
}
