package downward

import (
	"bytes"
	"io"

	envoybootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/structpb"

	// Register Envoy types used in bootstrap typed_config attributes before unmarshalling.
	_ "github.com/kgateway-dev/kgateway/v2/pkg/utils/filter_types"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/protoutils"
)

func Transform(in io.Reader, out io.Writer) error {
	api := RetrieveDownwardAPI()
	interpolator := NewInterpolator()

	var interpolated bytes.Buffer
	if err := interpolator.InterpolateIO(in, &interpolated, api); err != nil {
		return err
	}

	var bootstrap envoybootstrapv3.Bootstrap
	if err := protoutils.UnmarshalYaml(interpolated.Bytes(), &bootstrap); err != nil {
		return err
	}
	if err := TransformConfigTemplatesWithApi(&bootstrap, api); err != nil {
		return err
	}

	bootstrapBytes, err := protoutils.MarshalBytes(&bootstrap)
	if err != nil {
		return err
	}
	_, err = out.Write(bootstrapBytes)
	return err
}

func TransformConfigTemplatesWithApi(bootstrap *envoybootstrapv3.Bootstrap, api DownwardAPI) error {
	interpolator := NewInterpolator()
	if bootstrap.Node == nil {
		bootstrap.Node = &envoycorev3.Node{}
	}

	var err error

	interpolate := func(s *string) error { return interpolator.InterpolateString(s, api) }
	// interpolate the ID templates:
	err = interpolate(&bootstrap.GetNode().Cluster)
	if err != nil {
		return err
	}

	err = interpolate(&bootstrap.GetNode().Id)
	if err != nil {
		return err
	}

	if err := transformStruct(interpolate, bootstrap.GetNode().GetMetadata()); err != nil {
		return err
	}

	// Set node locality from environment variables if available.
	// These are typically set via KGATEWAY_NODE_ZONE, KGATEWAY_NODE_REGION, KGATEWAY_NODE_SUBZONE
	// environment variables on the Envoy proxy pod, populated from the node's topology labels.
	// This is required for Envoy's zone-aware routing (ZoneAwareLbConfig) to function.
	if zone, region, subzone := api.NodeZone(), api.NodeRegion(), api.NodeSubzone(); zone != "" || region != "" || subzone != "" {
		bootstrap.Node.Locality = &envoycorev3.Locality{
			Region:  region,
			Zone:    zone,
			SubZone: subzone,
		}
	}

	// Interpolate Static Resources
	for _, cluster := range bootstrap.GetStaticResources().GetClusters() {
		for _, endpoint := range cluster.GetLoadAssignment().GetEndpoints() {
			for _, lbEndpoint := range endpoint.GetLbEndpoints() {
				socketAddress := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()
				if socketAddress != nil {
					if err = interpolate(&socketAddress.Address); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func transformValue(interpolate func(*string) error, v *structpb.Value) error {
	switch v := v.GetKind().(type) {
	case *structpb.Value_StringValue:
		return interpolate(&v.StringValue)
	case *structpb.Value_StructValue:
		return transformStruct(interpolate, v.StructValue)
	case *structpb.Value_ListValue:
		for _, val := range v.ListValue.GetValues() {
			if err := transformValue(interpolate, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func transformStruct(interpolate func(*string) error, s *structpb.Struct) error {
	if s == nil {
		return nil
	}

	for _, v := range s.GetFields() {
		if err := transformValue(interpolate, v); err != nil {
			return err
		}
	}
	return nil
}
