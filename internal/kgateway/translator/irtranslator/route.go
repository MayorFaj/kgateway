package irtranslator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoy_type_matcher_v3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/reports"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/translator/routeutils"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils"
	"github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/ir"
	reportssdk "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk/reporter"
	"github.com/kgateway-dev/kgateway/v2/pkg/settings"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/regexutils"
)

type httpRouteConfigurationTranslator struct {
	gw               ir.GatewayIR
	listener         ir.ListenerIR
	fc               ir.FilterChainCommon
	attachedPolicies ir.AttachedPolicies

	routeConfigName          string
	reporter                 reportssdk.Reporter
	requireTlsOnVirtualHosts bool
	PluginPass               TranslationPassPlugins
	logger                   *slog.Logger
	routeReplacementMode     settings.RouteReplacementMode
}

const WebSocketUpgradeType = "websocket"

func (h *httpRouteConfigurationTranslator) ComputeRouteConfiguration(ctx context.Context, vhosts []*ir.VirtualHost) *envoy_config_route_v3.RouteConfiguration {
	var attachedPolicies ir.AttachedPolicies
	// the policies in order - first listener as they are more specific and thus higher priority.
	// then gateway policies.
	attachedPolicies.Append(h.attachedPolicies, h.gw.AttachedHttpPolicies)
	cfg := &envoy_config_route_v3.RouteConfiguration{
		Name: h.routeConfigName,
	}
	typedPerFilterConfigRoute := ir.TypedFilterConfigMap(map[string]proto.Message{})

	for _, gk := range attachedPolicies.ApplyOrderedGroupKinds() {
		pols := attachedPolicies.Policies[gk]
		pass := h.PluginPass[gk]
		if pass == nil {
			// TODO: user error - they attached a non http policy
			continue
		}
		reportPolicyAcceptanceStatus(h.reporter, h.listener.PolicyAncestorRef, pols...)
		for _, pol := range mergePolicies(pass, pols) {
			pass.ApplyRouteConfigPlugin(ctx, &ir.RouteConfigContext{
				FilterChainName:   h.fc.FilterChainName,
				TypedFilterConfig: typedPerFilterConfigRoute,
				Policy:            pol.PolicyIr,
			}, cfg)
		}
	}

	cfg.VirtualHosts = h.computeVirtualHosts(ctx, vhosts)
	cfg.TypedPerFilterConfig = toPerFilterConfigMap(typedPerFilterConfigRoute)

	// Gateway API spec requires that port values in HTTP Host headers be ignored when performing a match
	// See https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteSpec - hostnames field
	cfg.IgnorePortInHostMatching = true

	//	if mostSpecificVal := h.parentListener.GetRouteOptions().GetMostSpecificHeaderMutationsWins(); mostSpecificVal != nil {
	//		cfg.MostSpecificHeaderMutationsWins = mostSpecificVal.GetValue()
	//	}

	return cfg
}

func (h *httpRouteConfigurationTranslator) computeVirtualHosts(ctx context.Context, virtualHosts []*ir.VirtualHost) []*envoy_config_route_v3.VirtualHost {
	var envoyVirtualHosts []*envoy_config_route_v3.VirtualHost
	for _, virtualHost := range virtualHosts {
		envoyVirtualHosts = append(envoyVirtualHosts, h.computeVirtualHost(ctx, virtualHost))
	}
	return envoyVirtualHosts
}

func (h *httpRouteConfigurationTranslator) computeVirtualHost(
	ctx context.Context,
	virtualHost *ir.VirtualHost,
) *envoy_config_route_v3.VirtualHost {
	sanitizedName := utils.SanitizeForEnvoy(ctx, virtualHost.Name, "virtual host")

	var envoyRoutes []*envoy_config_route_v3.Route
	for i, route := range virtualHost.Rules {
		// TODO: not sure if we need listener parent ref here or the http parent ref
		var routeReport reportssdk.ParentRefReporter = &reports.ParentRefReport{}
		if route.Parent != nil {
			// route may be a fake one that we don't really report,
			// such as in the waypoint translator where we produce
			// synthetic routes if there none are attached to the Gateway/Service.
			routeReport = h.reporter.Route(route.Parent.SourceObject).ParentRef(&route.ParentRef)
		}
		generatedName := fmt.Sprintf("%s-route-%d", virtualHost.Name, i)
		computedRoute := h.envoyRoutes(ctx, routeReport, route, generatedName)
		if computedRoute != nil {
			envoyRoutes = append(envoyRoutes, computedRoute)
		}
	}
	domains := []string{virtualHost.Hostname}
	if len(domains) == 0 || (len(domains) == 1 && domains[0] == "") {
		domains = []string{"*"}
	}
	var envoyRequireTls envoy_config_route_v3.VirtualHost_TlsRequirementType
	if h.requireTlsOnVirtualHosts {
		// TODO (ilackarms): support external-only TLS
		envoyRequireTls = envoy_config_route_v3.VirtualHost_ALL
	}

	out := &envoy_config_route_v3.VirtualHost{
		Name:       sanitizedName,
		Domains:    domains,
		Routes:     envoyRoutes,
		RequireTls: envoyRequireTls,
	}

	typedPerFilterConfigRoute := ir.TypedFilterConfigMap(map[string]proto.Message{})
	// run the http plugins that are attached to the listener or gateway on the virtual host
	h.runVhostPlugins(ctx, virtualHost, out, typedPerFilterConfigRoute)
	out.TypedPerFilterConfig = toPerFilterConfigMap(typedPerFilterConfigRoute)

	return out
}

type backendConfigContext struct {
	typedPerFilterConfigRoute ir.TypedFilterConfigMap
	RequestHeadersToAdd       []*envoy_config_core_v3.HeaderValueOption
	RequestHeadersToRemove    []string
	ResponseHeadersToAdd      []*envoy_config_core_v3.HeaderValueOption
	ResponseHeadersToRemove   []string
}

func (h *httpRouteConfigurationTranslator) envoyRoutes(
	ctx context.Context,
	routeReport reportssdk.ParentRefReporter,
	in ir.HttpRouteRuleMatchIR,
	generatedName string,
) *envoy_config_route_v3.Route {
	out := h.initRoutes(in, generatedName)

	// Early conflict detection - check for incompatible filter combinations before applying any plugins
	if hasIncompatibleFilters(in) {
		// Return a 500 error response immediately for incompatible filters
		out.Action = &envoy_config_route_v3.Route_DirectResponse{
			DirectResponse: &envoy_config_route_v3.DirectResponseAction{
				Status: http.StatusInternalServerError,
			},
		}

		routeReport.SetCondition(reportssdk.RouteCondition{
			Type:    gwv1.RouteConditionAccepted,
			Status:  metav1.ConditionFalse,
			Reason:  gwv1.RouteReasonIncompatibleFilters,
			Message: "incompatible filters: multiple route actions detected",
		})

		return out
	}

	backendConfigCtx := backendConfigContext{typedPerFilterConfigRoute: ir.TypedFilterConfigMap(map[string]proto.Message{})}
	if len(in.Backends) == 1 {
		// if there's only one backend, we need to reuse typedPerFilterConfigRoute in both translateRouteAction and runRoutePlugins
		out.Action = h.translateRouteAction(ctx, in, out, &backendConfigCtx)
	} else if len(in.Backends) > 0 {
		// If there is more than one backend, we translate the backends as WeightedClusters and each weighted cluster
		// will have a TypedPerFilterConfig that overrides the parent route-level config.
		out.Action = h.translateRouteAction(ctx, in, out, nil)
	}

	// run plugins here that may set action
	err := h.runRoutePlugins(ctx, routeReport, in, out, backendConfigCtx.typedPerFilterConfigRoute)
	if err == nil {
		err = validateEnvoyRoute(out)
	}

	// apply typed per filter config from translating route action and route plugins
	typedPerFilterConfig := toPerFilterConfigMap(backendConfigCtx.typedPerFilterConfigRoute)
	if out.GetTypedPerFilterConfig() == nil {
		out.TypedPerFilterConfig = typedPerFilterConfig
	} else {
		for k, v := range typedPerFilterConfig {
			if _, exists := out.GetTypedPerFilterConfig()[k]; !exists {
				out.GetTypedPerFilterConfig()[k] = v
			}
		}
	}

	out.RequestHeadersToAdd = append(out.GetRequestHeadersToAdd(), backendConfigCtx.RequestHeadersToAdd...)
	out.RequestHeadersToRemove = append(out.GetRequestHeadersToRemove(), backendConfigCtx.RequestHeadersToRemove...)
	out.ResponseHeadersToAdd = append(out.GetResponseHeadersToAdd(), backendConfigCtx.ResponseHeadersToAdd...)
	out.ResponseHeadersToRemove = append(out.GetResponseHeadersToRemove(), backendConfigCtx.ResponseHeadersToRemove...)

	if err == nil && out.GetAction() == nil {
		if in.Delegates {
			return nil
		} else {
			// Check if this route has any extension refs that might indicate missing policies
			// If a route was expecting to get an action from an extension ref but has no action,
			// it's likely due to a missing/unresolved policy that should result in a 500
			hasMissingExtensionRefs := false
			for _, extRefGroup := range in.ExtensionRefs.Policies {
				for _, policy := range extRefGroup {
					if policy.PolicyIr == nil && len(policy.Errors) > 0 {
						hasMissingExtensionRefs = true
						break
					}
				}
				if hasMissingExtensionRefs {
					break
				}
			}

			if hasMissingExtensionRefs || (len(in.ExtensionRefs.Policies) == 0 && hasExpectedExtensionRefs(in)) {
				err = fmt.Errorf("missing extension policy: %w", ir.ErrMissingPolicy)
			} else {
				err = fmt.Errorf("no action specified")
			}
		}
	}
	if err != nil {
		h.logger.Debug("invalid route", "error", err)

		// Check if this is a plugin conflict error or missing policy error that should result in a 500 response
		shouldReturn500 := errors.Is(err, ir.ErrRouteActionConflict) ||
			errors.Is(err, ir.ErrMissingDirectResponse) ||
			errors.Is(err, ir.ErrIncompatibleFilters) ||
			errors.Is(err, ir.ErrMultipleRouteActions) ||
			errors.Is(err, ir.ErrConflictingRouteActions) ||
			errors.Is(err, ir.ErrMissingPolicy)

		if shouldReturn500 {
			// Replace the route with a 500 error response for incompatible filters or missing policies
			out.Reset()
			out.Match = h.initRoutes(in, generatedName).GetMatch()
			out.Name = h.initRoutes(in, generatedName).GetName()
			out.Action = &envoy_config_route_v3.Route_DirectResponse{
				DirectResponse: &envoy_config_route_v3.DirectResponseAction{
					Status: http.StatusInternalServerError,
				},
			}

			// Set appropriate route condition for missing policies
			if errors.Is(err, ir.ErrMissingPolicy) {
				routeReport.SetCondition(reportssdk.RouteCondition{
					Type:    gwv1.RouteConditionAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  gwv1.RouteReasonUnsupportedValue,
					Message: "missing or invalid extension policy",
				})
			}
			return out
		}

		// For other errors, set RouteConditionPartiallyInvalid
		routeReport.SetCondition(reportssdk.RouteCondition{
			Type:    gwv1.RouteConditionPartiallyInvalid,
			Status:  metav1.ConditionTrue,
			Reason:  gwv1.RouteReasonUnsupportedValue,
			Message: fmt.Sprintf("Dropped Rule (%d): %v", in.MatchIndex, err),
		})

		switch h.routeReplacementMode {
		case settings.RouteReplacementStandard, settings.RouteReplacementStrict:
			// Replace invalid route with a direct response
			out.Action = &envoy_config_route_v3.Route_DirectResponse{
				DirectResponse: &envoy_config_route_v3.DirectResponseAction{
					Status: http.StatusInternalServerError,
				},
			}
			return out
		default:
			// Drop the route entirely (legacy behavior, will be removed in the future)
			return nil
		}
	}

	return out
}

// hasExpectedExtensionRefs checks if this route was expecting to have extension refs
// by looking at the original HTTPRoute filters, backends, etc.
func hasExpectedExtensionRefs(in ir.HttpRouteRuleMatchIR) bool {
	for _, extRefGroup := range in.ExtensionRefs.Policies {
		if len(extRefGroup) > 0 {
			return true
		}
	}

	if len(in.Backends) == 0 && !in.Delegates {
		return true
	}

	return false
}

func toPerFilterConfigMap(typedPerFilterConfig ir.TypedFilterConfigMap) map[string]*anypb.Any {
	typedPerFilterConfigAny := map[string]*anypb.Any{}
	for k, v := range typedPerFilterConfig {
		if anyMsg, ok := v.(*anypb.Any); ok {
			typedPerFilterConfigAny[k] = anyMsg
			continue
		}
		config, err := utils.MessageToAny(v)
		if err != nil {
			// TODO: error on status? this should never happen..
			logger.Error("unexpected marshalling error", "error", err)
			continue
		}
		typedPerFilterConfigAny[k] = config
	}
	return typedPerFilterConfigAny
}

func (h *httpRouteConfigurationTranslator) runVhostPlugins(ctx context.Context, virtualHost *ir.VirtualHost, out *envoy_config_route_v3.VirtualHost,
	typedPerFilterConfig ir.TypedFilterConfigMap,
) {
	for _, gk := range virtualHost.AttachedPolicies.ApplyOrderedGroupKinds() {
		pols := virtualHost.AttachedPolicies.Policies[gk]
		pass := h.PluginPass[gk]
		if pass == nil {
			// TODO: user error - they attached a non http policy
			continue
		}
		reportPolicyAcceptanceStatus(h.reporter, h.listener.PolicyAncestorRef, pols...)
		for _, pol := range mergePolicies(pass, pols) {
			pctx := &ir.VirtualHostContext{
				Policy:            pol.PolicyIr,
				TypedFilterConfig: typedPerFilterConfig,
				FilterChainName:   h.fc.FilterChainName,
			}
			pass.ApplyVhostPlugin(ctx, pctx, out)
			// TODO: check return value, if error returned, log error and report condition
		}
	}
}

func (h *httpRouteConfigurationTranslator) runRoutePlugins(
	ctx context.Context,
	routeReport reportssdk.ParentRefReporter,
	in ir.HttpRouteRuleMatchIR,
	out *envoy_config_route_v3.Route,
	typedPerFilterConfig ir.TypedFilterConfigMap,
) error {
	// all policies up to listener have been applied as vhost polices; we need to apply the httproute policies and below
	//
	// NOTE: AttachedPolicies must have policies in the ordered by hierarchy from root to leaf in the delegation chain where
	// each level has policies ordered by rule level policies before entire route level policies.

	var attachedPolicies ir.AttachedPolicies
	delegatingParent := in.DelegatingParent
	var hierarchicalPriority int
	for delegatingParent != nil {
		hierarchicalPriority++
		attachedPolicies.Prepend(hierarchicalPriority,
			delegatingParent.ExtensionRefs, delegatingParent.AttachedPolicies, delegatingParent.Parent.AttachedPolicies)
		delegatingParent = delegatingParent.DelegatingParent
	}

	// rule-level policies in priority order (high to low)
	attachedPolicies.Append(in.ExtensionRefs, in.AttachedPolicies)

	// route-level policy
	if in.Parent != nil {
		attachedPolicies.Append(in.Parent.AttachedPolicies)
	}

	var errs []error
	var hasRouteActionConflictError bool

	// Early detection of missing policies - check for ErrorPolicyIR in ExtensionRefs
	// This ensures that missing policies cause immediate route failure rather than "no action specified"
	for _, policyGroup := range in.ExtensionRefs.Policies {
		for _, policy := range policyGroup {
			// Check if this is an ErrorPolicyIR (missing/unresolved policy)
			if policy.PolicyIr != nil {
				policyType := fmt.Sprintf("%T", policy.PolicyIr)
				if strings.Contains(policyType, "ErrorPolicyIR") {
					routeReport.SetCondition(reportssdk.RouteCondition{
						Type:    gwv1.RouteConditionAccepted,
						Status:  metav1.ConditionFalse,
						Reason:  gwv1.RouteReasonUnsupportedValue,
						Message: fmt.Sprintf("missing or invalid extension policy: %v", policy.FormatErrors()),
					})
					return fmt.Errorf("missing extension policy: %w", ir.ErrMissingPolicy)
				}
			}

			if policy.PolicyIr == nil && len(policy.Errors) > 0 {
				// This is a missing/unresolved policy - fail the route immediately
				routeReport.SetCondition(reportssdk.RouteCondition{
					Type:    gwv1.RouteConditionAccepted,
					Status:  metav1.ConditionFalse,
					Reason:  gwv1.RouteReasonUnsupportedValue,
					Message: fmt.Sprintf("missing or invalid extension policy: %v", policy.FormatErrors()),
				})
				return fmt.Errorf("missing extension policy: %w", ir.ErrMissingPolicy)
			}
		}
	}

	applyForPolicy := func(ctx context.Context, pass *TranslationPass, pctx *ir.RouteContext, out *envoy_config_route_v3.Route) {
		err := pass.ApplyForRoute(ctx, pctx, out)
		if err != nil {
			errs = append(errs, err)
			// Check if this is a route action conflict error using type-safe detection
			if errors.Is(err, ir.ErrRouteActionConflict) {
				hasRouteActionConflictError = true
			}
		}
	}
	for _, gk := range attachedPolicies.ApplyOrderedGroupKinds() {
		pols := attachedPolicies.Policies[gk]
		pass := h.PluginPass[gk]
		if pass == nil {
			// TODO: should never happen, log error and report condition
			continue
		}
		reportPolicyAcceptanceStatus(h.reporter, h.listener.PolicyAncestorRef, pols...)
		pctx := &ir.RouteContext{
			FilterChainName:   h.fc.FilterChainName,
			In:                in,
			TypedFilterConfig: typedPerFilterConfig,
		}
		for _, pol := range mergePolicies(pass, pols) {
			// Check for route action conflicts detected early in policy resolution
			if pol.PolicyIr == nil && len(pol.Errors) > 0 {
				for _, err := range pol.Errors {
					if strings.Contains(err.Error(), "incompatible filters: multiple route actions detected") {
						hasRouteActionConflictError = true
						errs = append(errs, err)
						// Set the condition immediately to mark this route as having incompatible filters
						routeReport.SetCondition(reportssdk.RouteCondition{
							Type:    gwv1.RouteConditionAccepted,
							Status:  metav1.ConditionFalse,
							Reason:  gwv1.RouteReasonIncompatibleFilters,
							Message: err.Error(),
						})
						return errors.Join(errs...)
					}
				}
			}

			if pol.PolicyIr == nil && len(pol.Errors) > 0 {
				errs = append(errs, pol.Errors...)
				continue
			}

			if len(pol.Errors) > 0 {
				errs = append(errs, pol.Errors...)
			}

			if pol.PolicyIr != nil {
				pctx.Policy = pol.PolicyIr
				applyForPolicy(ctx, pass, pctx, out)
			}
		}

		// TODO: check return value, if error returned, log error and report condition
	}

	err := errors.Join(errs...)
	if err != nil {
		// If we have route action conflicts, treat this as IncompatibleFilters
		if hasRouteActionConflictError {
			routeReport.SetCondition(reportssdk.RouteCondition{
				Type:    gwv1.RouteConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  gwv1.RouteReasonIncompatibleFilters,
				Message: err.Error(),
			})
		} else {
			// For other policy errors, treat as UnsupportedValue
			routeReport.SetCondition(reportssdk.RouteCondition{
				Type:    gwv1.RouteConditionAccepted,
				Status:  metav1.ConditionFalse,
				Reason:  gwv1.RouteReasonUnsupportedValue,
				Message: err.Error(),
			})
		}
	}

	return err
}

func mergePolicies(pass *TranslationPass, policies []ir.PolicyAtt) []ir.PolicyAtt {
	if pass.MergePolicies != nil {
		merged := [1]ir.PolicyAtt{pass.MergePolicies(policies)}
		return merged[:]
	}

	return policies
}

func (h *httpRouteConfigurationTranslator) runBackendPolicies(ctx context.Context, in ir.HttpBackend, pCtx *ir.RouteBackendContext) error {
	var errs []error
	for _, gk := range in.AttachedPolicies.ApplyOrderedGroupKinds() {
		pols := in.AttachedPolicies.Policies[gk]
		pass := h.PluginPass[gk]
		if pass == nil {
			// TODO: should never happen, log error and report condition
			continue
		}
		reportPolicyAcceptanceStatus(h.reporter, h.listener.PolicyAncestorRef, pols...)
		for _, pol := range mergePolicies(pass, pols) {
			// Policy on extension ref
			err := pass.ApplyForRouteBackend(ctx, pol.PolicyIr, pCtx)
			if err != nil {
				errs = append(errs, err)
			}
			// TODO: check return value, if error returned, log error and report condition
		}
	}
	return errors.Join(errs...)
}

func (h *httpRouteConfigurationTranslator) runBackend(ctx context.Context, in ir.HttpBackend, pCtx *ir.RouteBackendContext, outRoute *envoy_config_route_v3.Route) error {
	var errs []error
	if in.Backend.BackendObject != nil {
		backendPass := h.PluginPass[in.Backend.BackendObject.GetGroupKind()]
		if backendPass != nil {
			err := backendPass.ApplyForBackend(ctx, pCtx, in, outRoute)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	// TODO: check return value, if error returned, log error and report condition
	return errors.Join(errs...)
}

func (h *httpRouteConfigurationTranslator) translateRouteAction(
	ctx context.Context,
	in ir.HttpRouteRuleMatchIR,
	outRoute *envoy_config_route_v3.Route,
	parentBackendConfigCtx *backendConfigContext,
) *envoy_config_route_v3.Route_Route {
	var clusters []*envoy_config_route_v3.WeightedCluster_ClusterWeight

	for _, backend := range in.Backends {
		clusterName := backend.Backend.ClusterName

		// get backend for ref - we must do it to make sure we have permissions to access it.
		// also we need the service so we can translate its name correctly.
		cw := &envoy_config_route_v3.WeightedCluster_ClusterWeight{
			Name:   clusterName,
			Weight: wrapperspb.UInt32(backend.Backend.Weight),
		}

		backendConfigCtx := parentBackendConfigCtx
		if parentBackendConfigCtx == nil {
			backendConfigCtx = &backendConfigContext{typedPerFilterConfigRoute: ir.TypedFilterConfigMap(map[string]proto.Message{})}
		}

		pCtx := ir.RouteBackendContext{
			FilterChainName:   h.fc.FilterChainName,
			Backend:           backend.Backend.BackendObject,
			TypedFilterConfig: backendConfigCtx.typedPerFilterConfigRoute,
		}

		// non attached policy translation
		err := h.runBackend(
			ctx,
			backend,
			&pCtx,
			outRoute,
		)
		if err != nil {
			// TODO: error on status
			h.logger.Error("error processing backends", "error", err)
		}
		err = h.runBackendPolicies(
			ctx,
			backend,
			&pCtx,
		)
		if err != nil {
			// TODO: error on status
			h.logger.Error("error processing backends with policies", "error", err)
		}

		backendConfigCtx.RequestHeadersToAdd = pCtx.RequestHeadersToAdd
		backendConfigCtx.RequestHeadersToRemove = pCtx.RequestHeadersToRemove
		backendConfigCtx.ResponseHeadersToAdd = pCtx.ResponseHeadersToAdd
		backendConfigCtx.ResponseHeadersToRemove = pCtx.ResponseHeadersToRemove

		// Translating weighted clusters needs the typed per filter config on each cluster
		cw.TypedPerFilterConfig = toPerFilterConfigMap(backendConfigCtx.typedPerFilterConfigRoute)
		cw.RequestHeadersToAdd = backendConfigCtx.RequestHeadersToAdd
		cw.RequestHeadersToRemove = backendConfigCtx.RequestHeadersToRemove
		cw.ResponseHeadersToAdd = backendConfigCtx.ResponseHeadersToAdd
		cw.ResponseHeadersToRemove = backendConfigCtx.ResponseHeadersToRemove
		clusters = append(clusters, cw)
	}

	// TODO: i think envoy nacks if all weights are 0, we should error on that.
	action := outRoute.GetRoute()
	if action == nil {
		action = &envoy_config_route_v3.RouteAction{
			ClusterNotFoundResponseCode: envoy_config_route_v3.RouteAction_INTERNAL_SERVER_ERROR,
		}
	}

	routeAction := &envoy_config_route_v3.Route_Route{
		Route: action,
	}
	switch len(clusters) {
	// case 0:
	// TODO: we should never get here
	case 1:
		// Only set the cluster name if unspecified since a plugin may have set it.
		if action.GetCluster() == "" {
			action.ClusterSpecifier = &envoy_config_route_v3.RouteAction_Cluster{
				Cluster: clusters[0].GetName(),
			}
		}
		// Skip setting the typed per filter config here, set it in the envoyRoutes() after runRoutePlugins runs

	default:
		// Only set weighted clusters if unspecified since a plugin may have set it.
		if action.GetWeightedClusters() == nil {
			action.ClusterSpecifier = &envoy_config_route_v3.RouteAction_WeightedClusters{
				WeightedClusters: &envoy_config_route_v3.WeightedCluster{
					Clusters: clusters,
				},
			}
		}
	}

	for _, backend := range in.Backends {
		if back := backend.Backend.BackendObject; back != nil && back.AppProtocol == ir.WebSocketAppProtocol {
			// add websocket upgrade if not already present
			if !slices.ContainsFunc(action.GetUpgradeConfigs(), func(uc *envoy_config_route_v3.RouteAction_UpgradeConfig) bool {
				return uc.GetUpgradeType() == WebSocketUpgradeType
			}) {
				action.UpgradeConfigs = append(action.GetUpgradeConfigs(), &envoy_config_route_v3.RouteAction_UpgradeConfig{
					UpgradeType: WebSocketUpgradeType,
				})
			}
		}
	}
	return routeAction
}

func validateEnvoyRoute(r *envoy_config_route_v3.Route) error {
	var errs []error
	match := r.GetMatch()
	route := r.GetRoute()
	re := r.GetRedirect()
	validatePath(match.GetPath(), &errs)
	validatePath(match.GetPrefix(), &errs)
	validatePath(match.GetPathSeparatedPrefix(), &errs)
	validatePath(re.GetPathRedirect(), &errs)
	validatePath(re.GetHostRedirect(), &errs)
	validatePath(re.GetSchemeRedirect(), &errs)
	validatePrefixRewrite(route.GetPrefixRewrite(), &errs)
	validatePrefixRewrite(re.GetPrefixRewrite(), &errs)
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("error %s: %w", r.GetName(), errors.Join(errs...))
}

// creates Envoy routes for each matcher provided on our Gateway route
func (h *httpRouteConfigurationTranslator) initRoutes(
	in ir.HttpRouteRuleMatchIR,
	generatedName string,
) *envoy_config_route_v3.Route {
	//	if len(in.Matches) == 0 {
	//		return []*envoy_config_route_v3.Route{
	//			{
	//				Match: &envoy_config_route_v3.RouteMatch{
	//					PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{Prefix: "/"},
	//				},
	//			},
	//		}
	//	}

	out := &envoy_config_route_v3.Route{
		Match: translateGlooMatcher(in.Match),
	}
	name := in.Name
	if name != "" {
		out.Name = fmt.Sprintf("%s-%s-matcher-%d", generatedName, name, in.MatchIndex)
	} else {
		out.Name = fmt.Sprintf("%s-matcher-%d", generatedName, in.MatchIndex)
	}

	return out
}

func translateGlooMatcher(matcher gwv1.HTTPRouteMatch) *envoy_config_route_v3.RouteMatch {
	match := &envoy_config_route_v3.RouteMatch{
		Headers:         envoyHeaderMatcher(matcher.Headers),
		QueryParameters: envoyQueryMatcher(matcher.QueryParams),
	}
	if matcher.Method != nil {
		match.Headers = append(match.GetHeaders(), &envoy_config_route_v3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &envoy_config_route_v3.HeaderMatcher_StringMatch{
				StringMatch: &envoy_type_matcher_v3.StringMatcher{
					MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
						Exact: string(*matcher.Method),
					},
				},
			},
		})
	}

	setEnvoyPathMatcher(matcher, match)
	return match
}

var separatedPathRegex = regexp.MustCompile("^[^?#]+[^?#/]$")

func isValidPathSparated(path string) bool {
	// see envoy docs:
	//	Expect the value to not contain "?" or "#" and not to end in "/"
	return separatedPathRegex.MatchString(path)
}

func setEnvoyPathMatcher(match gwv1.HTTPRouteMatch, out *envoy_config_route_v3.RouteMatch) {
	pathType, pathValue := routeutils.ParsePath(match.Path)
	switch pathType {
	case gwv1.PathMatchPathPrefix:
		if !isValidPathSparated(pathValue) {
			out.PathSpecifier = &envoy_config_route_v3.RouteMatch_Prefix{
				Prefix: pathValue,
			}
		} else {
			out.PathSpecifier = &envoy_config_route_v3.RouteMatch_PathSeparatedPrefix{
				PathSeparatedPrefix: pathValue,
			}
		}
	case gwv1.PathMatchExact:
		out.PathSpecifier = &envoy_config_route_v3.RouteMatch_Path{
			Path: pathValue,
		}
	case gwv1.PathMatchRegularExpression:
		out.PathSpecifier = &envoy_config_route_v3.RouteMatch_SafeRegex{
			SafeRegex: regexutils.NewRegexWithProgramSize(pathValue, nil),
		}
	}
}

func envoyHeaderMatcher(in []gwv1.HTTPHeaderMatch) []*envoy_config_route_v3.HeaderMatcher {
	var out []*envoy_config_route_v3.HeaderMatcher
	for _, matcher := range in {
		envoyMatch := &envoy_config_route_v3.HeaderMatcher{
			Name: string(matcher.Name),
		}
		regex := false
		if matcher.Type != nil && *matcher.Type == gwv1.HeaderMatchRegularExpression {
			regex = true
		}

		// TODO: not sure if we should do PresentMatch according to the spec.
		if matcher.Value == "" {
			envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_PresentMatch{
				PresentMatch: true,
			}
		} else {
			if regex {
				envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_SafeRegex{
							SafeRegex: regexutils.NewRegexWithProgramSize(matcher.Value, nil),
						},
					},
				}
			} else {
				envoyMatch.HeaderMatchSpecifier = &envoy_config_route_v3.HeaderMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
							Exact: matcher.Value,
						},
					},
				}
			}
		}
		out = append(out, envoyMatch)
	}
	return out
}

func envoyQueryMatcher(in []gwv1.HTTPQueryParamMatch) []*envoy_config_route_v3.QueryParameterMatcher {
	var out []*envoy_config_route_v3.QueryParameterMatcher
	for _, matcher := range in {
		envoyMatch := &envoy_config_route_v3.QueryParameterMatcher{
			Name: string(matcher.Name),
		}
		regex := false
		if matcher.Type != nil && *matcher.Type == gwv1.QueryParamMatchRegularExpression {
			regex = true
		}

		// TODO: not sure if we should do PresentMatch according to the spec.
		if matcher.Value == "" {
			envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_PresentMatch{
				PresentMatch: true,
			}
		} else {
			if regex {
				envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_SafeRegex{
							SafeRegex: regexutils.NewRegexWithProgramSize(matcher.Value, nil),
						},
					},
				}
			} else {
				envoyMatch.QueryParameterMatchSpecifier = &envoy_config_route_v3.QueryParameterMatcher_StringMatch{
					StringMatch: &envoy_type_matcher_v3.StringMatcher{
						MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
							Exact: matcher.Value,
						},
					},
				}
			}
		}
		out = append(out, envoyMatch)
	}
	return out
}

func hasIncompatibleFilters(in ir.HttpRouteRuleMatchIR) bool {
	// Check for incompatible filter combinations that would result in multiple route actions

	hasDirectResponse := false
	hasRequestRedirect := false
	hasDelegation := in.Delegates || len(in.Backends) > 0

	// Check for RequestRedirect in the original HTTPRoute filters
	if in.Parent != nil && in.Parent.SourceObject != nil {
		// Check if the source object is an HTTPRoute
		if httpRoute, ok := in.Parent.SourceObject.(*gwv1.HTTPRoute); ok {
			// Look through the rules to find the matching rule
			for _, rule := range httpRoute.Spec.Rules {
				for _, filter := range rule.Filters {
					if filter.Type == gwv1.HTTPRouteFilterRequestRedirect {
						hasRequestRedirect = true
						break
					}
				}
				if hasRequestRedirect {
					break
				}
			}
		}
	}

	// Check attached policies and extension refs for RequestRedirect and DirectResponse filters
	checkPolicyConditions := func(policies []ir.PolicyAtt) {
		for _, policy := range policies {
			if policy.PolicyIr != nil {
				policyType := strings.ToLower(fmt.Sprintf("%T", policy.PolicyIr))
				if strings.Contains(policyType, "requestredirect") || (strings.Contains(policyType, "builtin") && strings.Contains(policyType, "redirect")) {
					hasRequestRedirect = true
				}
				// Use type switch for more reliable redirect detection
				switch policy.PolicyIr.(type) {
				case interface{ IsRequestRedirect() bool }:
					hasRequestRedirect = true
				case interface{ IsBuiltinRedirect() bool }:
					hasRequestRedirect = true
				}

				if isDirectResponse(policy.PolicyIr) {
					hasDirectResponse = true
				}
			}
		}
	}

	// Check attached policies directly on this route
	for _, policyGroup := range in.AttachedPolicies.Policies {
		checkPolicyConditions(policyGroup)
	}

	// Check parent attached policies if this is a delegation scenario
	if in.Parent != nil {
		for _, policyGroup := range in.Parent.AttachedPolicies.Policies {
			checkPolicyConditions(policyGroup)
		}
	}

	// Check extension refs for DirectResponse filters
	for _, extRef := range in.ExtensionRefs.Policies {
		checkPolicyConditions(extRef)
	}

	// Incompatible combinations:
	// 1. DirectResponse + RequestRedirect
	// 2. DirectResponse + Delegation (backends)
	if hasDirectResponse && hasRequestRedirect {
		return true
	}

	if hasDirectResponse && hasDelegation {
		return true
	}

	return false
}

// isDirectResponse checks if the given policy is a DirectResponse policy
func isDirectResponse(policyIr interface{}) bool {
	policyType := strings.ToLower(fmt.Sprintf("%T", policyIr))
	return strings.Contains(policyType, "directresponse")
}
