# Global Rate Limiting in kgateway

Global rate limiting allows you to apply distributed, consistent rate limits across multiple instances of your gateway. Unlike local rate limiting, which operates independently on each gateway instance, global rate limiting uses a central service to coordinate rate limits.

## Overview

Global rate limiting in kgateway is powered by [Envoy's rate limiting service protocol](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) and delegates rate limit decisions to an external service that implements this protocol. This approach provides several benefits:

- **Coordinated rate limiting** across multiple gateway instances
- **Centralized rate limit management** with shared counters
- **Dynamic descriptor-based rate limits** that can consider multiple request attributes
- **Consistent user experience** regardless of which gateway instance receives the request

## Architecture

The global rate limiting feature consists of three components:

1. **TrafficPolicy with global.rateLimit** - Configures rate limiting behaviors on routes or other Gateway API resources
2. **GatewayExtension** - References the rate limit service
3. **Rate Limit Service** - An external service that implements the Envoy Rate Limit protocol

## Deployment

### 1. Deploy the Rate Limit Service

kgateway integrates with any service that implements the Envoy Rate Limit gRPC protocol. For your convenience, we provide an example deployment using the official Envoy rate limit service in the [examples/global-rate-limiting](../examples/global-rate-limiting) directory.

```bash
kubectl apply -f examples/global-rate-limiting/rate-limit-service.yaml
```

### 2. Create a GatewayExtension

The GatewayExtension resource connects your kgateway installation with the rate limit service:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: GatewayExtension
metadata:
  name: global-ratelimit
  namespace: kgateway-system
spec:
  type: RateLimit
  rateLimit:
    grpcService:
      backendRef:
        name: ratelimit
        namespace: kgateway-system
        port: 8081
    timeout: "100ms"  # Optional timeout for rate limit service calls
```

### 3. Create TrafficPolicies with Global Rate Limiting

Apply rate limits to your routes using the TrafficPolicy resource:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: ip-rate-limit
  namespace: default
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  rateLimit:
    global:
      domain: "api-gateway"
      descriptors:
      - key: "remote_address"
        valueFrom:
          remoteAddress: true
      extensionRef:
        name: global-ratelimit
      failOpen: false
```

## Configuration Options

### TrafficPolicy.spec.rateLimit.global

| Field | Description | Required |
|-------|-------------|----------|
| domain | Identifies a rate limiting configuration domain | Yes |
| descriptors | Define the dimensions for rate limiting | No |
| requestsPerUnit | Maximum number of requests per time unit | No |
| unit | Time unit for the rate limit (second, minute, hour, day) | No |
| extensionRef | Reference to a GatewayExtension for the rate limit service | Yes |
| failOpen | When true, requests are not limited if the rate limit service is unavailable | No |

### Rate Limit Descriptors

Descriptors define the dimensions for rate limiting. Each descriptor represents a key-value pair that helps categorize and count requests:

```yaml
descriptors:
- key: "remote_address"
  valueFrom:
    remoteAddress: true
- key: "path"
  valueFrom:
    path: true
- key: "user_id"
  valueFrom:
    header: "X-User-ID"
- key: "service"
  value: "premium-api"
```

## Examples

The [examples/global-rate-limiting](../examples/global-rate-limiting) directory contains several examples:

1. **IP-based rate limiting**: Limit requests based on client IP address
2. **Path-based rate limiting**: Apply different limits to specific paths
3. **User-based rate limiting**: Rate limit based on a user identifier header
4. **Combined rate limiting**: Use both global and local rate limiting together

## Monitoring

When global rate limiting is active, kgateway will expose the following metrics:

- `envoy_cluster_ratelimit_over_limit` - Count of requests that were rate limited
- `envoy_cluster_ratelimit_ok` - Count of requests that were not rate limited
- `envoy_cluster_ratelimit_error` - Count of errors encountered while calling the rate limit service
- `envoy_cluster_ratelimit_failure_mode_allowed` - Count of requests that were allowed due to fail-open mode when the rate limit service was unavailable

## Troubleshooting

Common issues with global rate limiting:

1. **Rate limit service unavailable**: Check the deployment status and logs of your rate limit service.
2. **Misconfigured descriptors**: Ensure your descriptor keys match those expected by your rate limit service configuration.
3. **Incorrect domain**: The domain in TrafficPolicy must match a domain configured in your rate limit service.

For more details on configuring the rate limit service, refer to the [Envoy Rate Limiting documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter).