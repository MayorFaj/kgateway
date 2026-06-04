package downward_test

import (
	"bytes"
	"os"
	"strings"

	envoybootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoyclusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyendpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/types/known/structpb"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/kgateway-dev/kgateway/v2/internal/envoyinit/pkg/downward"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/protoutils"
)

var _ = Describe("Transform", func() {
	Context("bootstrap transforms", func() {
		var (
			api             *mockDownward
			bootstrapConfig *envoybootstrapv3.Bootstrap
		)
		BeforeEach(func() {
			api = &mockDownward{
				podName: "Test",
				nodeIp:  "5.5.5.5",
			}
			bootstrapConfig = new(envoybootstrapv3.Bootstrap)
			bootstrapConfig.Node = &envoycorev3.Node{}
		})

		It("should transform node id", func() {
			bootstrapConfig.Node.Id = "{{.PodName}}"
			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapConfig.Node.Id).To(Equal("Test"))
		})

		It("should transform cluster", func() {
			bootstrapConfig.Node.Cluster = "{{.PodName}}"
			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapConfig.Node.Cluster).To(Equal("Test"))
		})

		It("should transform metadata", func() {
			bootstrapConfig.Node.Metadata = &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"foo": {
						Kind: &structpb.Value_StringValue{
							StringValue: "{{.PodName}}",
						},
					},
				},
			}

			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapConfig.Node.Metadata.Fields["foo"].Kind.(*structpb.Value_StringValue).StringValue).To(Equal("Test"))
		})

		It("should set node locality", func() {
			api.nodeRegion = "us-east1"
			api.nodeZone = "us-east1-b"
			api.nodeSubzone = "rack-a"

			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapConfig.Node.Locality).To(Equal(&envoycorev3.Locality{
				Region:  "us-east1",
				Zone:    "us-east1-b",
				SubZone: "rack-a",
			}))
		})

		It("should initialize node when setting node locality", func() {
			bootstrapConfig.Node = nil
			api.nodeZone = "us-east1-b"

			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapConfig.Node).NotTo(BeNil())
			Expect(bootstrapConfig.Node.Locality).To(Equal(&envoycorev3.Locality{
				Zone: "us-east1-b",
			}))
		})

		It("should set node locality through the public IO transform", func() {
			setLocalityEnv("us-east1", "us-east1-b", "")

			transformed := transformBootstrapYaml(`
node:
  id: static
  cluster: static
`)
			Expect(transformed.Node.Locality).NotTo(BeNil())
			Expect(transformed.Node.Locality.Region).To(Equal("us-east1"))
			Expect(transformed.Node.Locality.Zone).To(Equal("us-east1-b"))
		})

		It("should preserve local cluster EDS wiring through the public IO transform", func() {
			setLocalityEnv("us-east1", "us-east1-b", "rack-a")

			transformed := transformBootstrapYaml(
				"node:\n" +
					"  id: static\n" +
					"  cluster: local-proxy\n" +
					"cluster_manager:\n" +
					"  local_cluster_name: local-proxy\n" +
					"static_resources:\n" +
					"  clusters:\n" +
					"  - name: local-proxy\n" +
					"    connect_timeout: 0.250s\n" +
					"    type: EDS\n" +
					"    lb_policy: ROUND_ROBIN\n" +
					"    eds_cluster_config:\n" +
					"      eds_config:\n" +
					"        ads: {}\n" +
					"        resource_api_version: V3\n",
			)
			Expect(transformed.GetClusterManager().GetLocalClusterName()).To(Equal("local-proxy"))
			Expect(transformed.GetNode().GetLocality().GetRegion()).To(Equal("us-east1"))
			Expect(transformed.GetNode().GetLocality().GetZone()).To(Equal("us-east1-b"))
			Expect(transformed.GetNode().GetLocality().GetSubZone()).To(Equal("rack-a"))
			localCluster := transformed.GetStaticResources().GetClusters()[0]
			Expect(localCluster.GetType()).To(Equal(envoyclusterv3.Cluster_EDS))
			Expect(localCluster.GetEdsClusterConfig().GetEdsConfig().GetAds()).ToNot(BeNil())
		})

		It("should preserve typed configs through the public IO transform", func() {
			transformed := transformBootstrapYaml(
				"static_resources:\n" +
					"  listeners:\n" +
					"  - name: listener-0\n" +
					"    address:\n" +
					"      socket_address:\n" +
					"        address: 0.0.0.0\n" +
					"        port_value: 8080\n" +
					"    filter_chains:\n" +
					"    - filters:\n" +
					"      - name: envoy.filters.network.http_connection_manager\n" +
					"        typed_config:\n" +
					"          '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager\n" +
					"          stat_prefix: ingress_http\n" +
					"          route_config:\n" +
					"            name: local_route\n" +
					"          http_filters:\n" +
					"          - name: envoy.filters.http.router\n" +
					"            typed_config:\n" +
					"              '@type': type.googleapis.com/envoy.extensions.filters.http.router.v3.Router\n",
			)
			filters := transformed.GetStaticResources().GetListeners()[0].GetFilterChains()[0].GetFilters()
			Expect(filters).To(HaveLen(1))
			Expect(filters[0].GetTypedConfig().GetTypeUrl()).To(Equal("type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"))
		})

		It("should transform static resources", func() {
			api.nodeRegion = "us-east1"
			api.nodeZone = "us-east1-b"
			api.nodeSubzone = "rack-a"
			bootstrapConfig.StaticResources = &envoybootstrapv3.Bootstrap_StaticResources{
				Clusters: []*envoyclusterv3.Cluster{{
					LoadAssignment: &envoyendpointv3.ClusterLoadAssignment{
						Endpoints: []*envoyendpointv3.LocalityLbEndpoints{{
							Locality: &envoycorev3.Locality{
								Region:  "{{.NodeRegion}}",
								Zone:    "{{.NodeZone}}",
								SubZone: "{{.NodeSubzone}}",
							},
							LbEndpoints: []*envoyendpointv3.LbEndpoint{{
								HostIdentifier: &envoyendpointv3.LbEndpoint_Endpoint{
									Endpoint: &envoyendpointv3.Endpoint{
										Address: &envoycorev3.Address{
											Address: &envoycorev3.Address_SocketAddress{
												SocketAddress: &envoycorev3.SocketAddress{
													Address: "{{.NodeIp}}",
												},
											},
										},
									},
								},
							}},
						}},
					},
				}},
			}

			err := TransformConfigTemplatesWithApi(bootstrapConfig, api)
			Expect(err).NotTo(HaveOccurred())

			expectedAddress := bootstrapConfig.GetStaticResources().GetClusters()[0].GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress().GetAddress()
			Expect(expectedAddress).To(Equal("5.5.5.5"))
			locality := bootstrapConfig.GetStaticResources().GetClusters()[0].GetLoadAssignment().GetEndpoints()[0].GetLocality()
			Expect(locality).To(Equal(&envoycorev3.Locality{
				Region:  "us-east1",
				Zone:    "us-east1-b",
				SubZone: "rack-a",
			}))
		})
	})
})

func setLocalityEnv(region, zone, subzone string) {
	GinkgoHelper()
	setEnvIfNotEmpty("KGATEWAY_NODE_REGION", region)
	setEnvIfNotEmpty("KGATEWAY_NODE_ZONE", zone)
	setEnvIfNotEmpty("KGATEWAY_NODE_SUBZONE", subzone)
}

func setEnvIfNotEmpty(name, value string) {
	GinkgoHelper()
	if value == "" {
		return
	}
	Expect(os.Setenv(name, value)).To(Succeed())
	DeferCleanup(os.Unsetenv, name)
}

func transformBootstrapYaml(input string) *envoybootstrapv3.Bootstrap {
	GinkgoHelper()

	var output bytes.Buffer
	Expect(Transform(strings.NewReader(input), &output)).To(Succeed())

	transformed := &envoybootstrapv3.Bootstrap{}
	Expect(protoutils.UnmarshalBytes(output.Bytes(), transformed)).To(Succeed())
	return transformed
}
