package trafficpolicy

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/reports"
	reportersdk "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/reporter"
)

// ErrorMockClient is a mock client that returns specific errors for certain resources
type ErrorMockClient struct {
	client.Client
	errorMap map[types.NamespacedName]error
}

// Get implements client.Client interface but returns specific errors for configured resources
func (c *ErrorMockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err, exists := c.errorMap[types.NamespacedName(key)]; exists {
		return err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// Watch implements client.WithWatch interface
func (c *ErrorMockClient) Watch(ctx context.Context, list client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
	return c.Client.(client.WithWatch).Watch(ctx, list, opts...)
}

// NewErrorMockClient creates a new mock client that returns specific errors for configured resources
func NewErrorMockClient(delegate client.Client, errorMap map[types.NamespacedName]error) *ErrorMockClient {
	return &ErrorMockClient{
		Client:   delegate,
		errorMap: errorMap,
	}
}

// Helper function for unit tests to create a PolicyReport without directly accessing unexported fields
func createTestPolicyReport(key reports.PolicyKey, observedGeneration int64, ancestors map[reports.ParentRefKey]*reports.AncestorRefReport) *reports.PolicyReport {
	rm := reports.NewReportMap()
	reporter := reports.NewReporter(&rm)

	// Create the base policy report
	policyReporter := reporter.Policy(key, observedGeneration)

	// Add each ancestor ref and condition
	for ancestorKey, ancestor := range ancestors {
		// Convert the ParentRefKey to a ParentReference
		parentRef := gwv1.ParentReference{
			Group:     ptr.To(gwv1.Group(ancestorKey.Group)),
			Kind:      ptr.To(gwv1.Kind(ancestorKey.Kind)),
			Name:      gwv1.ObjectName(ancestorKey.Name),
			Namespace: ptr.To(gwv1.Namespace(ancestorKey.Namespace)),
		}

		// Get the ancestor ref reporter
		ancestorReporter := policyReporter.AncestorRef(parentRef)

		// Add each condition
		for _, condition := range ancestor.Conditions {
			ancestorReporter.SetCondition(reportersdk.PolicyCondition{
				Type:    gwv1alpha2.PolicyConditionType(condition.Type),
				Status:  condition.Status,
				Reason:  gwv1alpha2.PolicyConditionReason(condition.Reason),
				Message: condition.Message,
			})
		}
	}

	return rm.Policies[key]
}

func TestTrafficPolicyUnattachedHandler(t *testing.T) {
	// Create a scheme for the fake client
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, gwv1.AddToScheme(scheme))

	testCases := []struct {
		name                   string
		existingPolicies       []client.Object
		existingHTTPRoutes     []client.Object
		clientErrors           map[types.NamespacedName]error
		existingReportMap      reports.ReportMap
		expectedPolicyStatuses map[types.NamespacedName][]reports.ParentRefKey
	}{
		{
			name: "policy with non-existent target",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-with-missing-target",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "non-existent-route",
							},
						},
					},
				},
			},
			existingHTTPRoutes: []client.Object{},
			existingReportMap:  reports.NewReportMap(),
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{
				{Name: "policy-with-missing-target", Namespace: "default"}: {
					{
						Group:          gwv1.GroupName,
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "non-existent-route", Namespace: "default"},
					},
				},
			},
		},
		{
			name: "policy with existing target",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-with-existing-target",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "existing-route",
							},
						},
					},
				},
			},
			existingHTTPRoutes: []client.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "gateway.networking.k8s.io/v1",
						"kind":       "HTTPRoute",
						"metadata": map[string]interface{}{
							"name":      "existing-route",
							"namespace": "default",
						},
					},
				},
			},
			existingReportMap:      reports.NewReportMap(),
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{},
		},
		{
			name: "policy with both existing and non-existent targets",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-with-mixed-targets",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "existing-route",
							},
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "non-existent-route",
							},
						},
					},
				},
			},
			existingHTTPRoutes: []client.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "gateway.networking.k8s.io/v1",
						"kind":       "HTTPRoute",
						"metadata": map[string]interface{}{
							"name":      "existing-route",
							"namespace": "default",
						},
					},
				},
			},
			existingReportMap: reports.NewReportMap(),
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{
				{Name: "policy-with-mixed-targets", Namespace: "default"}: {
					{
						Group:          gwv1.GroupName,
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "non-existent-route", Namespace: "default"},
					},
				},
			},
		},
		{
			name: "policy with status already in report map should be skipped",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-already-has-status",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "non-existent-route",
							},
						},
					},
				},
			},
			existingHTTPRoutes: []client.Object{},
			existingReportMap: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "gateway.kgateway.dev",
					Kind:      "TrafficPolicy",
					Namespace: "default",
					Name:      "policy-already-has-status",
				}

				// Create ancestor reports using the proper API
				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "Gateway",
						NamespacedName: types.NamespacedName{Name: "example-gateway", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionTrue,
								Reason:  string(gwv1alpha2.PolicyReasonAccepted),
								Message: reportersdk.PolicyAcceptedMsg,
							},
						},
					},
				}

				// Create the policy report using the helper function instead of direct struct assignment
				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{},
		},
		{
			name: "multiple policies referencing the same non-existent target",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-a-with-missing-target",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "shared-non-existent-route",
							},
						},
					},
				},
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-b-with-missing-target",
						Namespace:  "default",
						Generation: 2,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "shared-non-existent-route",
							},
						},
					},
				},
			},
			existingHTTPRoutes: []client.Object{},
			existingReportMap:  reports.NewReportMap(),
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{
				{Name: "policy-a-with-missing-target", Namespace: "default"}: {
					{
						Group:          gwv1.GroupName,
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "shared-non-existent-route", Namespace: "default"},
					},
				},
				{Name: "policy-b-with-missing-target", Namespace: "default"}: {
					{
						Group:          gwv1.GroupName,
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "shared-non-existent-route", Namespace: "default"},
					},
				},
			},
		},
		{
			name: "policy with target returning error other than not found",
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "policy-with-error-target",
						Namespace:  "default",
						Generation: 1,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "TrafficPolicy",
						APIVersion: "gateway.kgateway.dev/v1alpha1",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReference{
							{
								Group: gwv1.GroupName,
								Kind:  "HTTPRoute",
								Name:  "error-route",
							},
						},
					},
				},
			},
			clientErrors: map[types.NamespacedName]error{
				{Name: "error-route", Namespace: "default"}: fmt.Errorf("internal server error"),
			},
			existingHTTPRoutes: []client.Object{},
			existingReportMap:  reports.NewReportMap(),
			// We don't expect any status updates for non-NotFound errors
			expectedPolicyStatuses: map[types.NamespacedName][]reports.ParentRefKey{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fake client with the test objects
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.existingPolicies...).
				WithObjects(tc.existingHTTPRoutes...).
				Build()

				// Wrap the fake client with the ErrorMockClient if clientErrors are provided
			if tc.clientErrors != nil {
				k8sClient = NewErrorMockClient(k8sClient, tc.clientErrors)
			}

			// Create the handler
			handler := NewTrafficPolicyUnattachedHandler(k8sClient, "test-controller")

			// Call the handler with the existing report map
			result, err := handler.HandleUnattachedPolicies(context.Background(), tc.existingReportMap)
			require.NoError(t, err)

			// Check if the policies that should have status do have it
			for policyNN, expectedAncestorKeys := range tc.expectedPolicyStatuses {
				policyKey := reports.PolicyKey{
					Group:     "gateway.kgateway.dev",
					Kind:      "TrafficPolicy",
					Namespace: policyNN.Namespace,
					Name:      policyNN.Name,
				}

				// Get the policy report from the result
				policyReport, exists := result.Policies[policyKey]
				assert.True(t, exists, "Expected policy %s to have a status report", policyNN.String())
				if !exists {
					continue
				}

				// Check each expected ancestor ref has a status
				for _, ancestorKey := range expectedAncestorKeys {
					ancestorReport, hasAncestor := policyReport.Ancestors[ancestorKey]
					assert.True(t, hasAncestor, "Expected policy to have ancestor %v", ancestorKey)
					if !hasAncestor {
						continue
					}

					// Verify the conditions
					foundTargetNotFound := false
					for _, condition := range ancestorReport.Conditions {
						if condition.Type == string(gwv1alpha2.PolicyConditionAccepted) &&
							condition.Status == metav1.ConditionFalse &&
							condition.Reason == string(gwv1alpha2.PolicyReasonTargetNotFound) {
							// For the error case, check for the specific error message format
							if tc.clientErrors != nil {
								// Should contain the error message from the mock client
								assert.Contains(t, condition.Message, "Error accessing target",
									"Error message should indicate the type of error")
								assert.Contains(t, condition.Message, "internal server error",
									"Error message should contain the specific error text")
							} else {
								// For not found errors, should have the standard message
								assert.Equal(t, reportersdk.PolicyTargetNotFoundMsg, condition.Message,
									"NotFound errors should use the standard target not found message")
							}

							foundTargetNotFound = true
							break
						}
					}
					assert.True(t, foundTargetNotFound, "Expected to find condition with reason TargetNotFound and message about target not found")
				}
			}

			// Also verify policies that should NOT have status don't have it
			for _, policy := range tc.existingPolicies {
				policyMeta := policy.GetObjectKind().GroupVersionKind()
				policyNN := types.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}

				if _, shouldHaveStatus := tc.expectedPolicyStatuses[policyNN]; !shouldHaveStatus {
					// If this policy wasn't in the expected list and wasn't in the existing report map,
					// it shouldn't have a new status report (unless it already had one)
					policyKey := reports.PolicyKey{
						Group:     policyMeta.Group,
						Kind:      policyMeta.Kind,
						Namespace: policy.GetNamespace(),
						Name:      policy.GetName(),
					}

					// Skip if it already had a report in the existing map
					if _, existingReport := tc.existingReportMap.Policies[policyKey]; existingReport {
						continue
					}

					// Verify no new report was created for this policy
					for ancestorKey, report := range tc.expectedPolicyStatuses {
						for _, ref := range report {
							if ref.NamespacedName.Name == policy.GetName() &&
								ref.NamespacedName.Namespace == policy.GetNamespace() {
								t.Errorf("Policy %s should not have a status report with ancestor %v",
									policyNN.String(), ancestorKey)
							}
						}
					}
				}
			}
		})
	}
}
