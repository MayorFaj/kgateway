package proxy_syncer

import (
	"context"
	"fmt"

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
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/reports"
	reportssdk "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/reporter"
)

type ObjWithAttachedPolicies interface {
	GetAttachedPolicies() ir.AttachedPolicies
	GetObjectSource() ir.ObjectSource
}

var _ ObjWithAttachedPolicies = ir.BackendObjectIR{}

func generatePolicyReport[T ObjWithAttachedPolicies](in []T) reports.ReportMap {
	merged := reports.NewReportMap()
	reporter := reports.NewReporter(&merged)

	// iterate all backends and aggregate all policies attached to them
	// we track each attachment point of the policy to be tracked as an
	// ancestor for reporting status
	for _, obj := range in {
		for _, polAtts := range obj.GetAttachedPolicies().Policies {
			for _, polAtt := range polAtts {
				if polAtt.PolicyRef == nil {
					// the policyRef may be nil in the case of virtual plugins (e.g. istio settings)
					// since there's no real policy object, we don't need to generate status for it
					continue
				}

				key := reports.PolicyKey{
					Group:     polAtt.PolicyRef.Group,
					Kind:      polAtt.PolicyRef.Kind,
					Namespace: polAtt.PolicyRef.Namespace,
					Name:      polAtt.PolicyRef.Name,
				}
				ancestorRef := gwv1.ParentReference{
					Group:     ptr.To(gwv1.Group(obj.GetObjectSource().Group)),
					Kind:      ptr.To(gwv1.Kind(obj.GetObjectSource().Kind)),
					Namespace: ptr.To(gwv1.Namespace(obj.GetObjectSource().Namespace)),
					Name:      gwv1.ObjectName(obj.GetObjectSource().Name),
				}
				// Update the initial status
				r := reporter.Policy(key, polAtt.Generation).AncestorRef(ancestorRef)

				if len(polAtt.Errors) > 0 {
					r.SetCondition(reportssdk.PolicyCondition{
						Type:    gwv1alpha2.PolicyConditionAccepted,
						Status:  metav1.ConditionFalse,
						Reason:  gwv1alpha2.PolicyReasonInvalid,
						Message: polAtt.FormatErrors(),
					})
				} else {
					r.SetCondition(reportssdk.PolicyCondition{
						Type:    gwv1alpha2.PolicyConditionAccepted,
						Status:  metav1.ConditionTrue,
						Reason:  gwv1alpha2.PolicyReasonAccepted,
						Message: reportssdk.PolicyAcceptedAndAttachedMsg,
					})
				}
			}
		}
	}

	return merged
}

// UnattachedPolicyHandler checks if policies have any non-existent target references
// and creates appropriate status reports for them
type UnattachedPolicyHandler interface {
	// HandleUnattachedPolicies processes policies that aren't attached to any objects
	// because their targetRefs don't exist
	HandleUnattachedPolicies(ctx context.Context, resourcesReportMap reports.ReportMap) (reports.ReportMap, error)
}

// TrafficPolicyUnattachedHandler handles TrafficPolicy resources with non-existent targetRefs
type TrafficPolicyUnattachedHandler struct {
	client         client.Client
	controllerName string
}

// NewTrafficPolicyUnattachedHandler creates a new handler for TrafficPolicy resources
func NewTrafficPolicyUnattachedHandler(client client.Client, controllerName string) *TrafficPolicyUnattachedHandler {
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

			// Create an unstructured object to check if the target exists
			gvk := schema.GroupVersionKind{
				Group: string(targetRef.Group),
				Kind:  string(targetRef.Kind),
			}

			// Determine the API version based on the group and kind
			switch {
			case targetRef.Group == gwv1.GroupName && targetRef.Kind == "HTTPRoute":
				gvk.Version = "v1"
			default:
				gvk.Version = "v1alpha1"
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)

			// Check if the target exists in the policy's namespace
			err := h.client.Get(ctx, types.NamespacedName{
				Namespace: policy.Namespace,
				Name:      string(targetRef.Name),
			}, obj)

			// If there's an error, create a status report
			if err != nil {
				// Create status report for the missing targetRef
				policyReport := reporter.Policy(policyKey, policy.Generation)

				var message string
				var reason gwv1alpha2.PolicyConditionReason

				// Distinguish between different types of errors
				if apierrors.IsNotFound(err) {
					// Target genuinely doesn't exist
					message = reportssdk.PolicyTargetNotFoundMsg
					reason = gwv1alpha2.PolicyReasonInvalid
				} else {
					// Some other error occurred when trying to access the target
					message = fmt.Sprintf("Error accessing target: %s", err.Error())
					reason = gwv1alpha2.PolicyReasonInvalid
				}

				policyReport.AncestorRef(parentRef).SetCondition(reportssdk.PolicyCondition{
					Type:    gwv1alpha2.PolicyConditionAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  reason,
					Message: message,
				})
			}
		}
	}

	// Merge the unattached policy reports with the existing reports
	mergedReportMap := reports.MergeReportMaps(resourcesReportMap, unattachedReportMap)
	return mergedReportMap, nil
}
