// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

// LLMProviderApplyConfiguration represents a declarative configuration of the LLMProvider type for use
// with apply.
type LLMProviderApplyConfiguration struct {
	Provider           *SupportedLLMProviderApplyConfiguration `json:"provider,omitempty"`
	HostOverride       *HostApplyConfiguration                 `json:"hostOverride,omitempty"`
	PathOverride       *PathOverrideApplyConfiguration         `json:"pathOverride,omitempty"`
	AuthHeaderOverride *AuthHeaderOverrideApplyConfiguration   `json:"authHeaderOverride,omitempty"`
}

// LLMProviderApplyConfiguration constructs a declarative configuration of the LLMProvider type for use with
// apply.
func LLMProvider() *LLMProviderApplyConfiguration {
	return &LLMProviderApplyConfiguration{}
}

// WithProvider sets the Provider field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Provider field is set to the value of the last call.
func (b *LLMProviderApplyConfiguration) WithProvider(value *SupportedLLMProviderApplyConfiguration) *LLMProviderApplyConfiguration {
	b.Provider = value
	return b
}

// WithHostOverride sets the HostOverride field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the HostOverride field is set to the value of the last call.
func (b *LLMProviderApplyConfiguration) WithHostOverride(value *HostApplyConfiguration) *LLMProviderApplyConfiguration {
	b.HostOverride = value
	return b
}

// WithPathOverride sets the PathOverride field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the PathOverride field is set to the value of the last call.
func (b *LLMProviderApplyConfiguration) WithPathOverride(value *PathOverrideApplyConfiguration) *LLMProviderApplyConfiguration {
	b.PathOverride = value
	return b
}

// WithAuthHeaderOverride sets the AuthHeaderOverride field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the AuthHeaderOverride field is set to the value of the last call.
func (b *LLMProviderApplyConfiguration) WithAuthHeaderOverride(value *AuthHeaderOverrideApplyConfiguration) *LLMProviderApplyConfiguration {
	b.AuthHeaderOverride = value
	return b
}
