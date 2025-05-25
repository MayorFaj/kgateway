package diagnose

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
	"github.com/kgateway-dev/kgateway/v2/pkg/schemes"
)

func NewDiagnoseCommand() *cobra.Command {
	var kubeconfig string
	var namespace string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnose policy attachment issues",
		Long: `Diagnose provides detailed information about policy attachment status,
helping to understand why policies might not be working as expected.`,
	}

	policyCmd := &cobra.Command{
		Use:   "policy [policy-name]",
		Short: "Diagnose a specific policy or all policies",
		Long: `Diagnose policy attachment issues for TrafficPolicy resources.
Provides detailed information about target references, attachment status, and potential issues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return diagnosePolicies(cmd.Context(), kubeconfig, namespace, allNamespaces, args)
		},
	}

	policyCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	policyCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to search (default: current namespace)")
	policyCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Search all namespaces")

	cmd.AddCommand(policyCmd)
	return cmd
}

func diagnosePolicies(ctx context.Context, kubeconfig, namespace string, allNamespaces bool, args []string) error {
	// Create Kubernetes client
	var config *rest.Config
	var err error

	// Use the standard kubeconfig loading logic
	if kubeconfig == "" {
		// Use default kubeconfig loading rules
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	// Create GVR resolver for dynamic resource discovery
	gvrResolver, err := NewGVRResolver(config)
	if err != nil {
		fmt.Printf("Warning: failed to create GVR resolver, falling back to static mapping: %v\n", err)
		gvrResolver = nil
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	scheme := schemes.DefaultScheme()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Determine namespace scope
	var namespaces []string
	if allNamespaces {
		namespaces = []string{""} // Empty string means all namespaces
	} else if namespace != "" {
		namespaces = []string{namespace}
	} else {
		// Use current namespace from kubeconfig
		currentNs, err := getCurrentNamespace(kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to get current namespace: %w", err)
		}
		namespaces = []string{currentNs}
	}

	// Diagnose policies
	for _, ns := range namespaces {
		if err := diagnosePoliciesInNamespace(ctx, k8sClient, dynamicClient, gvrResolver, ns, args); err != nil {
			return err
		}
	}

	return nil
}

func diagnosePoliciesInNamespace(ctx context.Context, k8sClient client.Client, dynamicClient dynamic.Interface, gvrResolver *GVRResolver, namespace string, args []string) error {
	var policies []v1alpha1.TrafficPolicy

	if len(args) > 0 {
		// Diagnose specific policy
		policyName := args[0]
		var policy v1alpha1.TrafficPolicy
		err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      policyName,
			Namespace: namespace,
		}, &policy)
		if err != nil {
			return fmt.Errorf("failed to get policy %s/%s: %w", namespace, policyName, err)
		}
		policies = append(policies, policy)
	} else {
		// List all policies in namespace
		var policyList v1alpha1.TrafficPolicyList
		err := k8sClient.List(ctx, &policyList, client.InNamespace(namespace))
		if err != nil {
			return fmt.Errorf("failed to list policies in namespace %s: %w", namespace, err)
		}
		policies = policyList.Items
	}

	if len(policies) == 0 {
		fmt.Printf("No TrafficPolicies found in namespace %s\n", namespace)
		return nil
	}

	// Diagnose each policy
	for _, policy := range policies {
		if err := diagnosePolicy(ctx, k8sClient, dynamicClient, gvrResolver, &policy); err != nil {
			fmt.Printf("Error diagnosing policy %s/%s: %v\n", policy.Namespace, policy.Name, err)
		}
		fmt.Println()
	}

	return nil
}

func diagnosePolicy(ctx context.Context, k8sClient client.Client, dynamicClient dynamic.Interface, gvrResolver *GVRResolver, policy *v1alpha1.TrafficPolicy) error {
	fmt.Printf("=== Diagnosing TrafficPolicy: %s/%s ===\n", policy.Namespace, policy.Name)

	// Check policy status
	fmt.Println("\n📊 Policy Status:")
	if len(policy.Status.Ancestors) == 0 {
		fmt.Println("  ❌ No ancestors found - Policy is UNATTACHED")
	} else {
		fmt.Printf("  ✅ %d ancestor(s) found - Policy is ATTACHED\n", len(policy.Status.Ancestors))
		for i, ancestor := range policy.Status.Ancestors {
			fmt.Printf("    %d. %s %s/%s\n", i+1, string(*ancestor.AncestorRef.Kind),
				getNamespaceFromPtr(ancestor.AncestorRef.Namespace, policy.Namespace), ancestor.AncestorRef.Name)
		}
	}

	// Analyze target references
	fmt.Println("\n🎯 Target Reference Analysis:")
	for i, targetRef := range policy.Spec.TargetRefs {
		fmt.Printf("  Target %d: %s %s/%s\n", i+1, targetRef.Kind,
			policy.Namespace, targetRef.Name)

		if err := analyzeTargetRef(ctx, k8sClient, dynamicClient, gvrResolver, policy, targetRef); err != nil {
			fmt.Printf("    ❌ Error analyzing target: %v\n", err)
		}
	}

	// Check for common issues
	fmt.Println("\n🔍 Common Issues Check:")
	checkCommonIssues(policy)

	return nil
}

func analyzeTargetRef(ctx context.Context, k8sClient client.Client, dynamicClient dynamic.Interface, gvrResolver *GVRResolver, policy *v1alpha1.TrafficPolicy, targetRef v1alpha1.LocalPolicyTargetReferenceWithSectionName) error {
	targetNs := policy.Namespace

	// Check if target exists
	exists, err := checkTargetExists(ctx, k8sClient, dynamicClient, gvrResolver, targetRef, targetNs)
	if err != nil {
		return fmt.Errorf("failed to check target existence: %w", err)
	}

	if !exists {
		fmt.Printf("    ❌ Target does not exist\n")
		fmt.Printf("    💡 Create the target resource or check if it exists: kubectl get %s %s -n %s\n",
			strings.ToLower(string(targetRef.Kind)), targetRef.Name, targetNs)
		return nil
	}

	fmt.Printf("    ✅ Target exists\n")

	// For HTTPRoute targets, check if they attach to a Gateway
	if targetRef.Kind == "HTTPRoute" {
		return analyzeHTTPRouteTarget(ctx, k8sClient, string(targetRef.Name), targetNs)
	}

	return nil
}

func analyzeHTTPRouteTarget(ctx context.Context, k8sClient client.Client, routeName, targetNs string) error {
	// Get the HTTPRoute
	var route gwv1.HTTPRoute
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      routeName,
		Namespace: targetNs,
	}, &route)
	if err != nil {
		return fmt.Errorf("failed to get HTTPRoute: %w", err)
	}

	fmt.Printf("    📍 HTTPRoute has %d parent reference(s)\n", len(route.Spec.ParentRefs))

	if len(route.Spec.ParentRefs) == 0 {
		fmt.Printf("    ⚠️  HTTPRoute has no parentRefs - it won't attach to any Gateway\n")
		fmt.Printf("    💡 Add parentRefs to attach the route to a Gateway\n")
		return nil
	}

	// Check each parent reference
	for i, parentRef := range route.Spec.ParentRefs {
		parentNs := targetNs
		if parentRef.Namespace != nil {
			parentNs = string(*parentRef.Namespace)
		}

		fmt.Printf("      Parent %d: %s %s/%s\n", i+1,
			getKindFromPtr(parentRef.Kind, "Gateway"), parentNs, parentRef.Name)

		// Check if parent Gateway exists
		exists, err := checkGatewayExists(ctx, k8sClient, string(parentRef.Name), parentNs)
		if err != nil {
			fmt.Printf("        ❌ Error checking Gateway: %v\n", err)
			continue
		}

		if !exists {
			fmt.Printf("        ❌ Gateway does not exist\n")
			fmt.Printf("        💡 Create the Gateway or check if it exists: kubectl get gateway %s -n %s\n",
				parentRef.Name, parentNs)
		} else {
			fmt.Printf("        ✅ Gateway exists\n")
		}
	}

	return nil
}

func checkTargetExists(ctx context.Context, k8sClient client.Client, dynamicClient dynamic.Interface, gvrResolver *GVRResolver, targetRef v1alpha1.LocalPolicyTargetReferenceWithSectionName, namespace string) (bool, error) {
	targetName := string(targetRef.Name)

	switch targetRef.Kind {
	case "HTTPRoute":
		var route gwv1.HTTPRoute
		err := k8sClient.Get(ctx, client.ObjectKey{Name: targetName, Namespace: namespace}, &route)
		return !errors.IsNotFound(err), client.IgnoreNotFound(err)
	case "Gateway":
		var gw gwv1.Gateway
		err := k8sClient.Get(ctx, client.ObjectKey{Name: targetName, Namespace: namespace}, &gw)
		return !errors.IsNotFound(err), client.IgnoreNotFound(err)
	default:
		// Try dynamic discovery first if available
		var gvr schema.GroupVersionResource
		var err error

		if gvrResolver != nil {
			gvr, err = gvrResolver.GetGVRForKind(string(targetRef.Kind))
		} else {
			// Fallback to static mapping
			gvr, err = getGVRForKind(string(targetRef.Kind))
		}

		if err != nil {
			// For unknown kinds, assume they exist to avoid false positives
			fmt.Printf("        ⚠️  Unknown target kind '%s', assuming target exists\n", targetRef.Kind)
			fmt.Printf("        💡 Verify manually: kubectl get %s %s -n %s\n",
				strings.ToLower(string(targetRef.Kind)), targetName, namespace)
			return true, nil
		}

		_, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, targetName, metav1.GetOptions{})
		return !errors.IsNotFound(err), client.IgnoreNotFound(err)
	}
}

func checkGatewayExists(ctx context.Context, k8sClient client.Client, name, namespace string) (bool, error) {
	var gw gwv1.Gateway
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &gw)
	return !errors.IsNotFound(err), client.IgnoreNotFound(err)
}

func checkCommonIssues(policy *v1alpha1.TrafficPolicy) {
	issues := []string{}

	// Check for empty target refs
	if len(policy.Spec.TargetRefs) == 0 {
		issues = append(issues, "Policy has no targetRefs specified")
	}

	if len(issues) == 0 {
		fmt.Println("  ✅ No common issues detected")
	} else {
		for _, issue := range issues {
			fmt.Printf("  ⚠️  %s\n", issue)
		}
	}
}

func getCurrentNamespace(kubeconfig string) (string, error) {
	if kubeconfig == "" {
		return "default", nil
	}

	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return "default", nil // fallback to default
	}

	if config.CurrentContext == "" {
		return "default", nil
	}

	context := config.Contexts[config.CurrentContext]
	if context == nil || context.Namespace == "" {
		return "default", nil
	}

	return context.Namespace, nil
}

func getNamespaceFromPtr(ns *gwv1a2.Namespace, defaultNs string) string {
	if ns == nil {
		return defaultNs
	}
	return string(*ns)
}

func getKindFromPtr(kind *gwv1.Kind, defaultKind string) string {
	if kind == nil {
		return defaultKind
	}
	return string(*kind)
}

// GVRResolver provides dynamic resolution of Kinds to GVRs using API discovery
type GVRResolver struct {
	discoveryClient discovery.DiscoveryInterface
	kindToGVR       map[string]schema.GroupVersionResource
}

// NewGVRResolver creates a new GVR resolver using API discovery
func NewGVRResolver(config *rest.Config) (*GVRResolver, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	resolver := &GVRResolver{
		discoveryClient: discoveryClient,
		kindToGVR:       make(map[string]schema.GroupVersionResource),
	}

	// Build the kind to GVR mapping
	if err := resolver.buildKindToGVRMapping(); err != nil {
		return nil, fmt.Errorf("failed to build kind to GVR mapping: %w", err)
	}

	return resolver, nil
}

// buildKindToGVRMapping discovers all available resources and builds a mapping
func (r *GVRResolver) buildKindToGVRMapping() error {
	// Get all API groups
	apiGroupList, err := r.discoveryClient.ServerGroups()
	if err != nil {
		return fmt.Errorf("failed to get server groups: %w", err)
	}

	// Process core API (empty group)
	if err := r.processAPIGroup("", "v1"); err != nil {
		return fmt.Errorf("failed to process core API: %w", err)
	}

	// Process each API group
	for _, group := range apiGroupList.Groups {
		// Use preferred version, or latest if no preferred version
		version := group.PreferredVersion.Version
		if version == "" && len(group.Versions) > 0 {
			version = group.Versions[0].Version
		}

		if err := r.processAPIGroup(group.Name, version); err != nil {
			// Log but don't fail for individual groups
			fmt.Printf("Warning: failed to process API group %s/%s: %v\n", group.Name, version, err)
			continue
		}
	}

	return nil
}

// processAPIGroup processes a specific API group/version and adds resources to the mapping
func (r *GVRResolver) processAPIGroup(group, version string) error {
	var gv schema.GroupVersion
	if group == "" {
		gv = schema.GroupVersion{Version: version}
	} else {
		gv = schema.GroupVersion{Group: group, Version: version}
	}

	resourceList, err := r.discoveryClient.ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		return fmt.Errorf("failed to get resources for %s: %w", gv.String(), err)
	}

	for _, resource := range resourceList.APIResources {
		// Skip subresources (they contain '/')
		if strings.Contains(resource.Name, "/") {
			continue
		}

		gvr := schema.GroupVersionResource{
			Group:    group,
			Version:  version,
			Resource: resource.Name,
		}

		// Handle potential conflicts by preferring certain groups
		if existing, exists := r.kindToGVR[resource.Kind]; exists {
			// Prefer Gateway API resources over others
			if group == "gateway.networking.k8s.io" && existing.Group != "gateway.networking.k8s.io" {
				r.kindToGVR[resource.Kind] = gvr
			}
			// Prefer stable versions over alpha/beta
			if isStableVersion(version) && !isStableVersion(existing.Version) {
				r.kindToGVR[resource.Kind] = gvr
			}
		} else {
			r.kindToGVR[resource.Kind] = gvr
		}
	}

	return nil
}

// GetGVRForKind returns the GVR for a given kind
func (r *GVRResolver) GetGVRForKind(kind string) (schema.GroupVersionResource, error) {
	gvr, ok := r.kindToGVR[kind]
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown kind: %s", kind)
	}
	return gvr, nil
}

// isStableVersion checks if a version is considered stable (v1, v2, etc.)
func isStableVersion(version string) bool {
	return !strings.Contains(version, "alpha") && !strings.Contains(version, "beta")
}

func getGVRForKind(kind string) (schema.GroupVersionResource, error) {
	// Fallback map for common kinds (in case discovery fails)
	kindToGVR := map[string]schema.GroupVersionResource{
		"Service": {
			Group:    "",
			Version:  "v1",
			Resource: "services",
		},
		"HTTPRoute": {
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "httproutes",
		},
		"Gateway": {
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "gateways",
		},
	}

	gvr, ok := kindToGVR[kind]
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown kind: %s", kind)
	}
	return gvr, nil
}
