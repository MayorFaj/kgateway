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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
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

func TestTrafficPolicyUnattachedDetector(t *testing.T) {
	// Create a scheme for the fake client
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, gwv1.AddToScheme(scheme))

	testCases := []struct {
		name                    string
		existingPolicies        []client.Object
		existingHTTPRoutes      []client.Object
		existingGateways        []client.Object
		clientErrors            map[types.NamespacedName]error
		inputGroupKind          schema.GroupKind
		expectedUnattachedCount int
		expectedUnattachedNames []types.NamespacedName
	}{
		{
			name:           "wrong GroupKind should return empty list",
			inputGroupKind: schema.GroupKind{Group: "other.group", Kind: "OtherPolicy"},
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-policy",
						Namespace: "default",
					},
				},
			},
			expectedUnattachedCount: 0,
			expectedUnattachedNames: []types.NamespacedName{},
		},
		{
			name:           "policy with non-existent HTTPRoute target",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-missing-route",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "non-existent-route",
								},
							},
						},
					},
				},
			},
			existingHTTPRoutes:      []client.Object{},
			expectedUnattachedCount: 1,
			expectedUnattachedNames: []types.NamespacedName{
				{Name: "policy-with-missing-route", Namespace: "default"},
			},
		},
		{
			name:           "policy with existing HTTPRoute target",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-existing-route",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "existing-route",
								},
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
			expectedUnattachedCount: 0,
			expectedUnattachedNames: []types.NamespacedName{},
		},
		{
			name:           "policy with mixed existing and non-existent targets",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-mixed-targets",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "existing-route",
								},
							},
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "non-existent-route",
								},
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
			expectedUnattachedCount: 1,
			expectedUnattachedNames: []types.NamespacedName{
				{Name: "policy-with-mixed-targets", Namespace: "default"},
			},
		},
		{
			name:           "policy with non-existent Gateway target",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-missing-gateway",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.GatewayKind,
									Name:  "non-existent-gateway",
								},
							},
						},
					},
				},
			},
			existingGateways:        []client.Object{},
			expectedUnattachedCount: 1,
			expectedUnattachedNames: []types.NamespacedName{
				{Name: "policy-with-missing-gateway", Namespace: "default"},
			},
		},
		{
			name:           "multiple policies with unattached targets",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-a-unattached",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "missing-route-a",
								},
							},
						},
					},
				},
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-b-unattached",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "missing-route-b",
								},
							},
						},
					},
				},
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-c-attached",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "existing-route",
								},
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
			expectedUnattachedCount: 2,
			expectedUnattachedNames: []types.NamespacedName{
				{Name: "policy-a-unattached", Namespace: "default"},
				{Name: "policy-b-unattached", Namespace: "default"},
			},
		},
		{
			name:           "policy with target causing client error should be skipped",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-error-target",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: gwv1.GroupName,
									Kind:  wellknown.HTTPRouteKind,
									Name:  "error-route",
								},
							},
						},
					},
				},
			},
			clientErrors: map[types.NamespacedName]error{
				{Name: "error-route", Namespace: "default"}: fmt.Errorf("internal server error"),
			},
			expectedUnattachedCount: 0, // Errors other than NotFound should not count as unattached
			expectedUnattachedNames: []types.NamespacedName{},
		},
		{
			name:           "policy with unknown target kind should not be considered unattached",
			inputGroupKind: wellknown.TrafficPolicyGVK.GroupKind(),
			existingPolicies: []client.Object{
				&v1alpha1.TrafficPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-with-unknown-kind",
						Namespace: "default",
					},
					Spec: v1alpha1.TrafficPolicySpec{
						TargetRefs: []v1alpha1.LocalPolicyTargetReferenceWithSectionName{
							{
								LocalPolicyTargetReference: v1alpha1.LocalPolicyTargetReference{
									Group: "unknown.group",
									Kind:  "UnknownKind",
									Name:  "some-resource",
								},
							},
						},
					},
				},
			},
			expectedUnattachedCount: 0, // Unknown kinds are assumed to exist to avoid false positives
			expectedUnattachedNames: []types.NamespacedName{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create all objects for the fake client
			allObjects := append(tc.existingPolicies, tc.existingHTTPRoutes...)
			allObjects = append(allObjects, tc.existingGateways...)

			// Create a fake client with the test objects
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(allObjects...).
				Build()

			// Wrap the fake client with the ErrorMockClient if clientErrors are provided
			if tc.clientErrors != nil {
				k8sClient = NewErrorMockClient(k8sClient, tc.clientErrors)
			}

			// Create the detector
			detector := NewTrafficPolicyUnattachedDetector(k8sClient)

			// Call the detector
			unattachedPolicies, err := detector.DetectUnattachedPolicies(context.Background(), tc.inputGroupKind)
			require.NoError(t, err)

			// Verify the count matches
			assert.Equal(t, tc.expectedUnattachedCount, len(unattachedPolicies),
				"Expected %d unattached policies, got %d", tc.expectedUnattachedCount, len(unattachedPolicies))

			// Verify the expected policies are in the result
			actualNames := make(map[types.NamespacedName]bool)
			for _, policy := range unattachedPolicies {
				actualNames[policy] = true
			}

			for _, expectedName := range tc.expectedUnattachedNames {
				assert.True(t, actualNames[expectedName],
					"Expected policy %s to be in unattached list", expectedName.String())
			}

			// Verify no unexpected policies are in the result
			for actualName := range actualNames {
				found := false
				for _, expectedName := range tc.expectedUnattachedNames {
					if actualName == expectedName {
						found = true
						break
					}
				}
				assert.True(t, found,
					"Unexpected policy %s found in unattached list", actualName.String())
			}
		})
	}
}
