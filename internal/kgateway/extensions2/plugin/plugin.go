package plugin

import "github.com/kgateway-dev/kgateway/v2/pkg/pluginsdk"

type (
	Plugin              = pluginsdk.Plugin
	ProcessBackend      = pluginsdk.ProcessBackend
	BackendPlugin       = pluginsdk.BackendPlugin
	KGwTranslator       = pluginsdk.KGwTranslator
	EndpointPlugin      = pluginsdk.EndpointPlugin
	AttachmentPoints    = pluginsdk.AttachmentPoints
	ContributesPolicies = pluginsdk.ContributesPolicies
	PolicyPlugin        = pluginsdk.PolicyPlugin
	PolicyReport        = pluginsdk.PolicyReport
	GetPolicyStatusFn   = pluginsdk.GetPolicyStatusFn
	PatchPolicyStatusFn = pluginsdk.PatchPolicyStatusFn
)

const (
	BackendAttachmentPoint = pluginsdk.BackendAttachmentPoint
	GatewayAttachmentPoint = pluginsdk.GatewayAttachmentPoint
	RouteAttachmentPoint   = pluginsdk.RouteAttachmentPoint
)

// GetProxySyncer returns the proxy syncer instance.
// It's used by plugins to register their custom unattached policy handlers.
func GetProxySyncer() interface{} {
	return proxySyncerInstance
}

// SetProxySyncer is used to store the proxy syncer instance
// so that plugins can access it during initialization.
func SetProxySyncer(ps interface{}) {
	proxySyncerInstance = ps
}

// proxySyncerInstance stores the singleton ProxySyncer instance
var proxySyncerInstance interface{}
