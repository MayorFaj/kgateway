# Multipool AI Backend Path Overrides

This document describes how to configure different path URLs for failover providers in multipool AI backend configurations.

## Overview

With multipool AI backends, you can now specify different path URLs for each provider in the failover chain. This is useful when different AI providers or custom endpoints require different API paths.

## Configuration

### Basic Multipool Configuration

Here's an example of a multipool AI backend with different path overrides for each failover provider:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: Backend
metadata:
  labels:
    app: kgateway
  name: openai-multipool
  namespace: gwtest
spec:
  type: AI
  ai:
    multipool:
      priorities:
        - pool:
            - pathOverride:
                fullPath: "/api/v1/custom/completions"
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: openai-secret-one
                  model: gpt-4o
        - pool:
            - pathOverride:
                fullPath: "/api/v2/fallback/completions" 
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: openai-secret-two
                  model: gpt-4.0-turbo
        - pool:
            - pathOverride:
                fullPath: "/v1/chat/completions"
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: openai-secret-three
                  model: gpt-3.5-turbo
```

### Mixed Provider Types with Path Overrides

You can also use path overrides with different provider types:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: Backend
metadata:
  name: mixed-ai-backend
  namespace: gwtest
spec:
  type: AI
  ai:
    multipool:
      priorities:
        - pool:
            - pathOverride:
                fullPath: "/api/v1/custom/openai"
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: openai-secret
                  model: gpt-4
        - pool:
            - pathOverride:
                fullPath: "/api/v1/custom/anthropic"
              provider:
                anthropic:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: anthropic-secret
                  model: claude-3-opus-20240229
```

### Host and Path Overrides

You can combine host overrides with path overrides for completely custom endpoints:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: Backend
metadata:
  name: custom-endpoints-backend
  namespace: gwtest
spec:
  type: AI
  ai:
    multipool:
      priorities:
        - pool:
            - hostOverride:
                host: custom-ai-provider.example.com
                port: 8443
              pathOverride:
                fullPath: "/api/v1/completions"
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: custom-provider-secret
                  model: custom-model
        - pool:
            - hostOverride:
                host: fallback-ai.example.com
                port: 443
              pathOverride:
                fullPath: "/v2/chat/completions"
              provider:
                openai:
                  authToken:
                    kind: SecretRef
                    secretRef:
                      name: fallback-provider-secret
                  model: fallback-model
```

## How It Works

1. **Per-Endpoint Path Configuration**: Each provider in a multipool can have its own `pathOverride` configuration
2. **Failover Behavior**: When the primary provider fails, traffic automatically routes to the next priority with its specific path configuration
3. **Template Resolution**: The transformation template checks for endpoint-specific path overrides first, then falls back to the default provider path
4. **Backward Compatibility**: Existing multipool configurations without path overrides continue to work unchanged

## Use Cases

- **Custom AI Providers**: Route to different custom AI endpoints with provider-specific paths
- **API Versioning**: Use different API versions for primary and fallback providers
- **Load Balancing**: Distribute load across different endpoints with custom routing paths
- **Testing**: Route traffic to test endpoints with different path configurations during development

## Important Notes

- Path overrides are applied per-endpoint, allowing fine-grained control over routing
- All providers in a multipool must still be of the same type (e.g., all OpenAI, all Anthropic)
- Path overrides work with all supported AI providers (OpenAI, Anthropic, Azure OpenAI, Gemini, Vertex AI)
- This feature complements existing HTTPRoute-level URLRewrite functionality for backend-specific path routing
