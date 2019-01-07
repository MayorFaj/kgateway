// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: github.com/solo-io/gloo/projects/gloo/api/v1/artifact.proto

package v1 // import "github.com/solo-io/gloo/projects/gloo/pkg/api/v1"

import proto "github.com/gogo/protobuf/proto"
import fmt "fmt"
import math "math"
import _ "github.com/gogo/protobuf/gogoproto"
import core "github.com/solo-io/solo-kit/pkg/api/v1/resources/core"

import bytes "bytes"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion2 // please upgrade the proto package

//
// @solo-kit:resource.short_name=art
// @solo-kit:resource.plural_name=artifacts
// @solo-kit:resource.resource_groups=api.gloo.solo.io
//
// Gloo Artifacts are used by Gloo to store small bits of binary or file data.
//
// Certain plugins such as the gRPC plugin read and write artifacts to one of Gloo's configured
// storage layer.
//
// Artifacts can be backed by files on disk, Kubernetes ConfigMaps, and Consul Key/Value pairs.
//
// Supported artifact backends can be selected in Gloo's boostrap options.
type Artifact struct {
	// Raw data data being stored
	Data string `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
	// Metadata contains the object metadata for this resource
	Metadata             core.Metadata `protobuf:"bytes,7,opt,name=metadata,proto3" json:"metadata"`
	XXX_NoUnkeyedLiteral struct{}      `json:"-"`
	XXX_unrecognized     []byte        `json:"-"`
	XXX_sizecache        int32         `json:"-"`
}

func (m *Artifact) Reset()         { *m = Artifact{} }
func (m *Artifact) String() string { return proto.CompactTextString(m) }
func (*Artifact) ProtoMessage()    {}
func (*Artifact) Descriptor() ([]byte, []int) {
	return fileDescriptor_artifact_4d49b488f1cb9eae, []int{0}
}
func (m *Artifact) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Artifact.Unmarshal(m, b)
}
func (m *Artifact) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Artifact.Marshal(b, m, deterministic)
}
func (dst *Artifact) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Artifact.Merge(dst, src)
}
func (m *Artifact) XXX_Size() int {
	return xxx_messageInfo_Artifact.Size(m)
}
func (m *Artifact) XXX_DiscardUnknown() {
	xxx_messageInfo_Artifact.DiscardUnknown(m)
}

var xxx_messageInfo_Artifact proto.InternalMessageInfo

func (m *Artifact) GetData() string {
	if m != nil {
		return m.Data
	}
	return ""
}

func (m *Artifact) GetMetadata() core.Metadata {
	if m != nil {
		return m.Metadata
	}
	return core.Metadata{}
}

func init() {
	proto.RegisterType((*Artifact)(nil), "gloo.solo.io.Artifact")
}
func (this *Artifact) Equal(that interface{}) bool {
	if that == nil {
		return this == nil
	}

	that1, ok := that.(*Artifact)
	if !ok {
		that2, ok := that.(Artifact)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		return this == nil
	} else if this == nil {
		return false
	}
	if this.Data != that1.Data {
		return false
	}
	if !this.Metadata.Equal(&that1.Metadata) {
		return false
	}
	if !bytes.Equal(this.XXX_unrecognized, that1.XXX_unrecognized) {
		return false
	}
	return true
}

func init() {
	proto.RegisterFile("github.com/solo-io/gloo/projects/gloo/api/v1/artifact.proto", fileDescriptor_artifact_4d49b488f1cb9eae)
}

var fileDescriptor_artifact_4d49b488f1cb9eae = []byte{
	// 202 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0xb2, 0x4e, 0xcf, 0x2c, 0xc9,
	0x28, 0x4d, 0xd2, 0x4b, 0xce, 0xcf, 0xd5, 0x2f, 0xce, 0xcf, 0xc9, 0xd7, 0xcd, 0xcc, 0xd7, 0x4f,
	0xcf, 0xc9, 0xcf, 0xd7, 0x2f, 0x28, 0xca, 0xcf, 0x4a, 0x4d, 0x2e, 0x29, 0x86, 0xf0, 0x12, 0x0b,
	0x32, 0xf5, 0xcb, 0x0c, 0xf5, 0x13, 0x8b, 0x4a, 0x32, 0xd3, 0x12, 0x93, 0x4b, 0xf4, 0x0a, 0x8a,
	0xf2, 0x4b, 0xf2, 0x85, 0x78, 0x40, 0x72, 0x7a, 0x20, 0x6d, 0x7a, 0x99, 0xf9, 0x52, 0x22, 0xe9,
	0xf9, 0xe9, 0xf9, 0x60, 0x09, 0x7d, 0x10, 0x0b, 0xa2, 0x46, 0xca, 0x10, 0x8b, 0x05, 0x60, 0x3a,
	0x3b, 0xb3, 0x04, 0x66, 0x6c, 0x6e, 0x6a, 0x49, 0x62, 0x4a, 0x62, 0x49, 0x22, 0x44, 0x8b, 0x52,
	0x04, 0x17, 0x87, 0x23, 0xd4, 0x22, 0x21, 0x21, 0x2e, 0x16, 0x90, 0x8c, 0x04, 0xa3, 0x02, 0xa3,
	0x06, 0x67, 0x10, 0x98, 0x2d, 0x64, 0xc1, 0xc5, 0x01, 0xd3, 0x21, 0xc1, 0xae, 0xc0, 0xa8, 0xc1,
	0x6d, 0x24, 0xa6, 0x97, 0x9c, 0x5f, 0x94, 0x0a, 0x73, 0x89, 0x9e, 0x2f, 0x54, 0xd6, 0x89, 0xe5,
	0xc4, 0x3d, 0x79, 0x86, 0x20, 0xb8, 0x6a, 0x27, 0xb3, 0x15, 0x8f, 0xe4, 0x18, 0xa3, 0x0c, 0x88,
	0xf3, 0x73, 0x41, 0x76, 0x3a, 0xd4, 0x81, 0x49, 0x6c, 0x60, 0x87, 0x19, 0x03, 0x02, 0x00, 0x00,
	0xff, 0xff, 0x12, 0x24, 0x2b, 0x7b, 0x2e, 0x01, 0x00, 0x00,
}
