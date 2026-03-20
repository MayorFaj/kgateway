# Introduction to kgateway-Specific Policy + Unit Tests

## Table of Contents

- [Slide 1: The Gap Gateway API Intentionally Leaves](#slide-1-the-gap-gateway-api-intentionally-leaves)
- [Slide 2: How kgateway Fills That Gap — TrafficPolicy](#slide-2-how-kgateway-fills-that-gap--trafficpolicy)
- [Slide 3: What TrafficPolicy Supports](#slide-3-what-trafficpolicy-supports)
- [Slide 4: The Translation Pipeline](#slide-4-the-translation-pipeline)
- [Slide 5: How We Test Translation — Golden File Tests](#slide-5-how-we-test-translation--golden-file-tests)
- [Slide 6: Run the Translator Unit Test Locally](#slide-6-run-the-translator-unit-test-locally)
- [Hands-On Exercise: Break It, Find It, Fix It](#hands-on-exercise-break-it-find-it-fix-it)

---

## The Gap Gateway API Intentionally Leaves

The Gateway API defines several resource types. The most common ones you will encounter are:

```
GatewayClass   — which implementation handles the Gateway (e.g. kgateway)
Gateway        — the front door (addresses, listeners, TLS)
HTTPRoute      — HTTP routing rules (which path goes to which Service)
GRPCRoute      — gRPC routing rules
TLSRoute       — TLS passthrough routing
TCPRoute       — TCP routing (experimental)
```

That covers **routing**. Production systems need more:

| Need | Example |
|------|---------|
| Rate limiting | Max 100 requests/second per client |
| Authentication | Only allow requests with a valid JWT |
| CORS | Tell browsers which origins are permitted |
| Request transformation | Strip a header before forwarding |
| Fault injection | Inject delays for chaos testing |

The Gateway API spec deliberately does not define how these work.
It leaves room for each implementation to add these features via their own CRDs.

This is the **Policy Attachment** pattern:
- You create a vendor-specific policy resource.
- You point it at a Gateway, HTTPRoute, or other supported resource via `targetRefs`.
- The controller applies the behavior.

---

## How kgateway Fills That Gap — TrafficPolicy

kgateway's primary policy CRD is `TrafficPolicy`.

Here is a minimal example attaching rate limiting to a route:

```yaml
apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: api-rate-limit
  namespace: team-a
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: my-api-route          # which HTTPRoute to attach to
  rateLimit:
    local:
      tokenBucket:
        maxTokens: 100            # burst capacity
        tokensPerFill: 10         # tokens added each interval
        fillInterval: 1s          # refill every 1 second = 10 req/s steady state
```

Key idea: `targetRefs` is what links the policy to the route.
Without it the policy does nothing.

The `targetRefs` mechanism is standardized across all Gateway API implementations.
The contents of `spec` (the actual policy fields) are kgateway-specific.

---

## What TrafficPolicy Supports

`TrafficPolicy` supports many fields — here are some of them:

| Field | What it does |
|-------|-------------|
| `rateLimit` | Token-bucket (local) or external service (global) rate limiting |
| `cors` | CORS headers and preflight handling |
| `extAuth` | Delegate auth to an external authorization service |
| `jwtAuth` | Validate JWTs, extract claims to headers |
| `transformation` | Rewrite request/response headers and bodies |
| `retry` | Automatic retries with backoff |
| `timeouts` | Per-route request timeouts |
| `basicAuth` / `apiKeyAuth` | Simple authentication methods |

kgateway also provides other policy CRDs for different concerns:

| CRD | Purpose |
|-----|---------|
| `HTTPListenerPolicy` | Access logging, custom error responses |
| `ListenerPolicy` | Proxy protocol, connection limits |
| `Backend` | Custom backend types (AWS Lambda, static upstreams) |
| `BackendConfigPolicy` | Health checks, circuit breaking |
| `GatewayParameters` | Customize the Envoy proxy deployment itself |
| `DirectResponse` | Return a static HTTP response without forwarding to a backend |
| `GatewayExtension` | Reference external services used by policies (rate limit server, ext-auth server) |

---

## The Translation Pipeline

When you apply a `TrafficPolicy`, kgateway translates it in three steps before Envoy ever sees it:

```
Your YAML  →  IR (internal model)  →  Merge with routes  →  xDS (Envoy config)  →  Envoy
```

> **IR (Intermediate Representation)** is kgateway's internal Go struct — a middle step between your human-readable YAML and Envoy's low-level config format. Think of it as a blueprint: not the final product, but structured enough to work with and easy to test.

1. **CRD -> IR**: The plugin converts your YAML into an internal Go struct (IR). Close to Envoy's format, but not yet.
2. **Policy Attachment**: kgateway merges the IR onto the matching HTTPRoute or Gateway listener.
3. **IR -> xDS**: The final Envoy configuration is generated and streamed to Envoy — no restart needed.

The IR step is what makes translation testable: the golden file tests verify step 3 by comparing the xDS output against a saved expected file.

---

##  How We Test Translation — Golden File Tests

kgateway uses golden file tests (also called snapshot tests) to verify translation.

How they work:

1. You write an input YAML (Gateway + HTTPRoute + TrafficPolicy etc.).
2. The test runs that through the translator.
3. The output xDS config is compared to a saved expected output file (the golden file).
4. If the output differs from the golden file, the test fails.

Where these live:

```
pkg/kgateway/translator/gateway/gateway_translator_test.go   <- test runner
pkg/kgateway/translator/gateway/testutils/inputs/            <- input YAMLs
pkg/kgateway/translator/gateway/testutils/outputs/           <- golden files
```

When you add a new feature or fix a bug:

```bash
# Regenerate golden files after making intentional changes
REFRESH_GOLDEN=true go test ./pkg/kgateway/translator/gateway/...
```

Then inspect the output diff to confirm the change is exactly what you intended.


---

## Hands-On Exercise: Break It, Find It, Fix It

> **Background**: Now that we understand how plugins translate `TrafficPolicy` into Envoy config, let's look at a real bug. There's a mistake in the global rate limit plugin — the kind you might make in a real PR. Envoy would quietly receive invalid config, and rate limiting would silently do nothing. Your job: run the test, read the diff, find the bug, fix it.

### Step 1: Check out the exercise branch

```bash
git fetch origin
git checkout kubeconeu-2026-contribfest
```

### Step 2: Run the failing test

```bash
go test ./pkg/kgateway/translator/gateway/... \
  -v -run "TestBasic/TrafficPolicy_RateLimit_Full_Config"
```

You should see a test failure. Look at the diff carefully — `-` lines are what the golden file expects, `+` lines are what the code actually produces.

> **Tip**: Scan the diff for field names that look subtly wrong.

### Step 3: Understand the test

The test reads this input file:
```
pkg/kgateway/translator/gateway/testutils/inputs/traffic-policy/rate-limit-full-config.yaml
```
and compares the translator output against:
```
pkg/kgateway/translator/gateway/testutils/outputs/traffic-policy/rate-limit-full-config.yaml
```

Find the `rateLimit.global` section in the input. Notice:

```yaml
rateLimit:
  global:
    descriptors:
    - entries:
      - type: Path
```

The `type: Path` entry tells Envoy to rate limit based on the request path.

### Step 4: Find the bug

Open the global rate limit plugin:
```
pkg/kgateway/extensions2/plugins/trafficpolicy/global_rate_limit_plugin.go
```

Look at the `createRateLimitActions` function. Find the `case kgateway.RateLimitDescriptorEntryTypePath:` block.

What `HeaderName` value is being set? What should it be?

<details>
<summary>Hint</summary>
The header name should be <code>:path</code>.
</details>

### Step 5: Fix it

Make the fix, then re-run the test:

```bash
go test ./pkg/kgateway/translator/gateway/... \
  -v -run "TestBasic/TrafficPolicy_RateLimit_Full_Config"
```

### Step 6: Verify all rate limit tests pass

```bash
go test ./pkg/kgateway/translator/gateway/... \
  -v -run "TestBasic/TrafficPolicy.*[Rr]ate"
```
