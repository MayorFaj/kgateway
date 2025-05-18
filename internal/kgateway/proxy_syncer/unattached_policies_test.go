package proxy_syncer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

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

func TestMergeReportMaps(t *testing.T) {
	// Test cases for the MergeReportMaps function
	testCases := []struct {
		name     string
		mapA     reports.ReportMap
		mapB     reports.ReportMap
		expected reports.ReportMap
	}{
		{
			name: "merge maps with different policies",
			mapA: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-a",
				}

				// Create ancestor reports using proper API
				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "Gateway",
						NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionTrue,
								Reason:  string(gwv1alpha2.PolicyReasonAccepted),
								Message: "Accepted",
							},
						},
					},
				}

				// Create policy report using the helper function
				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			mapB: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-b",
				}

				// Create ancestor reports using proper API
				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionFalse,
								Reason:  string(gwv1alpha2.PolicyReasonTargetNotFound),
								Message: reportersdk.PolicyTargetNotFoundMsg,
							},
						},
					},
				}

				// Create policy report using the helper function
				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			expected: func() reports.ReportMap {
				rm := reports.NewReportMap()
				// Policy A
				policyKeyA := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-a",
				}

				ancestorsA := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "Gateway",
						NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionTrue,
								Reason:  string(gwv1alpha2.PolicyReasonAccepted),
								Message: "Accepted",
							},
						},
					},
				}

				rm.Policies[policyKeyA] = createTestPolicyReport(policyKeyA, 1, ancestorsA)

				// Policy B
				policyKeyB := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-b",
				}

				ancestorsB := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionFalse,
								Reason:  string(gwv1alpha2.PolicyReasonTargetNotFound),
								Message: reportersdk.PolicyTargetNotFoundMsg,
							},
						},
					},
				}

				rm.Policies[policyKeyB] = createTestPolicyReport(policyKeyB, 1, ancestorsB)
				return rm
			}(),
		},
		{
			name: "merge maps with overlapping policies",
			mapA: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-same",
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "Gateway",
						NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionTrue,
								Reason:  string(gwv1alpha2.PolicyReasonAccepted),
								Message: "Accepted",
							},
						},
					},
				}

				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			mapB: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-same",
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionFalse,
								Reason:  string(gwv1alpha2.PolicyReasonTargetNotFound),
								Message: reportersdk.PolicyTargetNotFoundMsg,
							},
						},
					},
				}

				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			expected: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-same",
				}

				// Create the merged ancestors
				gatewayAncestor := &reports.AncestorRefReport{
					Conditions: []metav1.Condition{
						{
							Type:    string(gwv1alpha2.PolicyConditionAccepted),
							Status:  metav1.ConditionTrue,
							Reason:  string(gwv1alpha2.PolicyReasonAccepted),
							Message: "Accepted",
						},
					},
				}

				routeAncestor := &reports.AncestorRefReport{
					Conditions: []metav1.Condition{
						{
							Type:    string(gwv1alpha2.PolicyConditionAccepted),
							Status:  metav1.ConditionFalse,
							Reason:  string(gwv1alpha2.PolicyReasonTargetNotFound),
							Message: reportersdk.PolicyTargetNotFoundMsg,
						},
					},
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "Gateway",
						NamespacedName: types.NamespacedName{Name: "gateway", Namespace: "default"},
					}: gatewayAncestor,
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: routeAncestor,
				}

				// Create the policy report
				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
		},
		{
			name: "merge maps with overlapping ancestor refs",
			mapA: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-overlap",
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    string(gwv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionTrue,
								Reason:  string(gwv1alpha2.PolicyReasonAccepted),
								Message: "Accepted",
							},
						},
					},
				}

				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			mapB: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-overlap",
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: {
						Conditions: []metav1.Condition{
							{
								Type:    "NewCondition",
								Status:  metav1.ConditionFalse,
								Reason:  "NewReason",
								Message: "New message",
							},
						},
					},
				}

				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
			expected: func() reports.ReportMap {
				rm := reports.NewReportMap()
				policyKey := reports.PolicyKey{
					Group:     "test.group",
					Kind:      "TestPolicy",
					Namespace: "default",
					Name:      "policy-overlap",
				}

				// Create the merged ancestor with both conditions
				mergedAncestor := &reports.AncestorRefReport{
					Conditions: []metav1.Condition{
						{
							Type:    string(gwv1alpha2.PolicyConditionAccepted),
							Status:  metav1.ConditionTrue,
							Reason:  string(gwv1alpha2.PolicyReasonAccepted),
							Message: "Accepted",
						},
						{
							Type:    "NewCondition",
							Status:  metav1.ConditionFalse,
							Reason:  "NewReason",
							Message: "New message",
						},
					},
				}

				ancestors := map[reports.ParentRefKey]*reports.AncestorRefReport{
					{
						Group:          "gateway.networking.k8s.io",
						Kind:           "HTTPRoute",
						NamespacedName: types.NamespacedName{Name: "route", Namespace: "default"},
					}: mergedAncestor,
				}

				// Create the policy report
				rm.Policies[policyKey] = createTestPolicyReport(policyKey, 1, ancestors)
				return rm
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := reports.MergeReportMaps(tc.mapA, tc.mapB)

			// Verify policies in result match expected
			assert.Equal(t, len(tc.expected.Policies), len(result.Policies))

			for policyKey, expectedReport := range tc.expected.Policies {
				resultReport, exists := result.Policies[policyKey]
				assert.True(t, exists, "Expected policy key %v in result", policyKey)
				if !exists {
					continue
				}

				// Check ancestors
				assert.Equal(t, len(expectedReport.Ancestors), len(resultReport.Ancestors))
				for ancestorKey, expectedAncestor := range expectedReport.Ancestors {
					resultAncestor, ancestorExists := resultReport.Ancestors[ancestorKey]
					assert.True(t, ancestorExists, "Expected ancestor key %v in result", ancestorKey)
					if !ancestorExists {
						continue
					}

					// Check conditions (ignoring LastTransitionTime)
					assert.Equal(t, len(expectedAncestor.Conditions), len(resultAncestor.Conditions))

					// Create maps of conditions by type for easier comparison
					expectedConditions := make(map[string]metav1.Condition)
					for _, cond := range expectedAncestor.Conditions {
						expectedConditions[cond.Type] = cond
					}

					resultConditions := make(map[string]metav1.Condition)
					for _, cond := range resultAncestor.Conditions {
						resultConditions[cond.Type] = cond
					}

					for condType, expectedCond := range expectedConditions {
						resultCond, condExists := resultConditions[condType]
						assert.True(t, condExists, "Expected condition type %s in result", condType)
						if condExists {
							assert.Equal(t, expectedCond.Status, resultCond.Status)
							assert.Equal(t, expectedCond.Reason, resultCond.Reason)
							assert.Equal(t, expectedCond.Message, resultCond.Message)
						}
					}
				}
			}
		})
	}
}

func TestCreateUnattachedPolicyReport(t *testing.T) {
	t.Run("creates report for unattached policy", func(t *testing.T) {
		policyKey := reports.PolicyKey{
			Group:     "test.group",
			Kind:      "TestPolicy",
			Namespace: "default",
			Name:      "unattached-policy",
		}

		targetRefs := []gwv1.ParentReference{
			{
				Group:     ptr.To(gwv1.Group("gateway.networking.k8s.io")),
				Kind:      ptr.To(gwv1.Kind("HTTPRoute")),
				Name:      "non-existent-route",
				Namespace: ptr.To(gwv1.Namespace("default")),
			},
		}

		// Create report using proper API
		reportMap := reports.NewReportMap()
		reporter := reports.NewReporter(&reportMap)

		// Create policy report without accessing unexported fields
		policyReporter := reporter.Policy(policyKey, 1)

		// For each targetRef, create an ancestorRef report
		for _, targetRef := range targetRefs {
			policyReporter.AncestorRef(targetRef).SetCondition(reportersdk.PolicyCondition{
				Type:    gwv1alpha2.PolicyConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  gwv1alpha2.PolicyReasonTargetNotFound,
				Message: reportersdk.PolicyTargetNotFoundMsg,
			})
		}

		// Verify
		assert.NotNil(t, reportMap)
		assert.Contains(t, reportMap.Policies, policyKey)

		policyReport := reportMap.Policies[policyKey]
		assert.NotNil(t, policyReport)

		// Check ancestor refs
		parentRefKey := reports.ParentRefKey{
			Group:          "gateway.networking.k8s.io",
			Kind:           "HTTPRoute",
			NamespacedName: types.NamespacedName{Name: "non-existent-route", Namespace: "default"},
		}
		assert.Contains(t, policyReport.Ancestors, parentRefKey)

		// Check conditions
		ancestorReport := policyReport.Ancestors[parentRefKey]
		assert.NotNil(t, ancestorReport)

		found := false
		for _, cond := range ancestorReport.Conditions {
			if cond.Type == string(gwv1alpha2.PolicyConditionAccepted) &&
				cond.Status == metav1.ConditionFalse &&
				cond.Reason == string(gwv1alpha2.PolicyReasonTargetNotFound) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find TargetNotFound condition")
	})
}
