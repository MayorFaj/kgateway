package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// GlobalRateLimitPolicy defines the configuration for global rate limiting
type GlobalRateLimitPolicy struct {
	// Domain identifies a rate limiting configuration. Multiple domains can be independently configured.
	Domain string `json:"domain"`

	// Descriptors define the dimensions for rate limiting.
	// +optional
	Descriptors []RateLimitDescriptor `json:"descriptors,omitempty"`

	// RequestsPerUnit is the maximum number of requests per time unit.
	// +optional
	RequestsPerUnit int32 `json:"requestsPerUnit,omitempty"`

	// Unit is the time unit for the rate limit.
	Unit string `json:"unit,omitempty"`

	// ExtensionRef is a reference to a GatewayExtension that provides the global rate limit service.
	ExtensionRef *corev1.LocalObjectReference `json:"extensionRef"`

	// When true, requests are not limited if the rate limit service is unavailable.
	// +optional
	FailOpen bool `json:"failOpen,omitempty"`
}

// RateLimitDescriptor is a single entry that defines a dimension for rate limiting
type RateLimitDescriptor struct {
	// Key for the descriptor entry.
	Key string `json:"key"`

	// Value for the descriptor entry.
	// +optional
	Value string `json:"value,omitempty"`

	// ValueFrom defines dynamic sources for the descriptor value.
	// +optional
	ValueFrom *RateLimitValueSource `json:"valueFrom,omitempty"`
}

// RateLimitValueSource defines sources for dynamic values in rate limit descriptors
type RateLimitValueSource struct {
	// Extract value from a request header.
	// +optional
	Header string `json:"header,omitempty"`

	// Use the remote address as the value.
	// +optional
	RemoteAddress bool `json:"remoteAddress,omitempty"`

	// Use the request path as the value.
	// +optional
	Path bool `json:"path,omitempty"`
}

// Update the RateLimitPolicy to include global rate limiting
type RateLimitPolicy struct {
	// Local defines local rate limiting configuration.
	// +optional
	Local *LocalRateLimitPolicy `json:"local,omitempty"`

	// Global defines global rate limiting configuration.
	// +optional
	Global *GlobalRateLimitPolicy `json:"global,omitempty"`
}
