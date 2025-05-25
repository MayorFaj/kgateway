# kgateway Policy Diagnostics

This document describes how to use the kgateway policy diagnostic capabilities in both development and production environments.

## Development Usage

When working with the source code locally, you can use the convenient Make targets:

```bash
# Diagnose all policies in all namespaces (default)
make diagnose

# Diagnose a specific policy
make diagnose DIAGNOSE_ARGS="policy gw-policy -n kgateway-test"

# Diagnose all policies in a specific namespace
make diagnose DIAGNOSE_ARGS="policy -n kgateway-test"

# Show help
make diagnose DIAGNOSE_ARGS="policy --help"
```

## Production Usage

In production environments where kgateway is deployed via Helm, there are several ways to access the diagnostic functionality:

### kubectl Plugin

Install the standalone kubectl plugin for the best user experience:

#### Installation

1. **Download the binary** for your platform from the kgateway releases page:

   ```bash
   # For Linux
   curl -L -o kubectl-kgateway https://github.com/kgateway-dev/kgateway/releases/download/v1.0.0/kubectl-kgateway-linux-amd64
   
   # For macOS
   curl -L -o kubectl-kgateway https://github.com/kgateway-dev/kgateway/releases/download/v1.0.0/kubectl-kgateway-darwin-amd64
   
   # For Windows
   curl -L -o kubectl-kgateway.exe https://github.com/kgateway-dev/kgateway/releases/download/v1.0.0/kubectl-kgateway-windows-amd64.exe
   ```

2. **Make it executable and move to PATH**:

   ```bash
   chmod +x kubectl-kgateway
   sudo mv kubectl-kgateway /usr/local/bin/
   ```

3. **Verify installation**:

   ```bash
   kubectl kgateway policy --help
   ```

#### Usage

Once installed, you can use the plugin like any other kubectl command:

```bash
# Diagnose all policies across all namespaces
kubectl kgateway policy --all-namespaces

# Diagnose a specific policy
kubectl kgateway policy gw-policy -n kgateway-test

# Diagnose all policies in a namespace
kubectl kgateway policy -n kgateway-test

# Use custom kubeconfig
kubectl kgateway policy --kubeconfig ~/.kube/prod-config --all-namespaces
```

## Command Examples and Output

### Scenario 1: Unattached Policy (Missing Target)

```bash
$ kubectl kgateway policy gw-policy -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/gw-policy ===

📊 Policy Status:
  ❌ No ancestors found - Policy is UNATTACHED

🎯 Target Reference Analysis:
  Target 1: HTTPRoute kgateway-test/simple-route
    ❌ Target does not exist
    💡 Create the target resource or check if it exists:: kubectl get httproute simple-route -n kgateway-test

🔍 Common Issues Check:
  ✅ No common issues detected
```

### Scenario 2: Policy with Missing Gateway

```bash
$ kubectl kgateway policy gw-policy -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/gw-policy ===

📊 Policy Status:
  ❌ No ancestors found - Policy is UNATTACHED

🎯 Target Reference Analysis:
  Target 1: HTTPRoute kgateway-test/simple-route
    ✅ Target exists
    📍 HTTPRoute has 1 parent reference(s)
      Parent 1: Gateway kgateway-test/super-gateway
        ❌ Gateway does not exist
        💡 Create the Gateway or check if it exists: kubectl get gateway super-gateway -n kgateway-test

🔍 Common Issues Check:
  ✅ No common issues detected
```

### Scenario 3: Successfully Attached Policy with Target References

```bash
$ kubectl kgateway policy gw-policy -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/gw-policy ===

📊 Policy Status:
  ✅ 1 ancestor(s) found - Policy is ATTACHED
    1. Gateway kgateway-test/super-gateway

🎯 Target Reference Analysis:
  Target 1: HTTPRoute kgateway-test/simple-route
    ✅ Target exists
    📍 HTTPRoute has 1 parent reference(s)
      Parent 1: Gateway kgateway-test/super-gateway
        ✅ Gateway exists

🔍 Common Issues Check:
  ✅ No common issues detected
```

### Scenario 4: Policy with Target Selectors (Mixed Results)

```bash
$ kubectl kgateway policy transform -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/transform ===

📊 Policy Status:
  ✅ 1 ancestor(s) found - Policy is ATTACHED
    1. Gateway kgateway-test/super-gateway

🎯 Target Reference Analysis:

🏷️ Target Selector Analysis:
  Selector 1: HTTPRoute with labels map[route:simple-route]
    ✅ Found 1 matching target(s)
      1. simple-route
    📍 HTTPRoute has 1 parent reference(s)
      Parent 1: Gateway kgateway-test/super-gateway
        ✅ Gateway exists
  Selector 2: HTTPRoute with labels map[route:simple-route-non-existent]
    ❌ No targets match selector
    💡 Create resources with matching labels or check existing resources: kubectl get httproute -l route=simple-route-non-existent -n kgateway-test

🔍 Common Issues Check:
  ✅ No common issues detected
```

### Scenario 5: Policy with Both Target References and Selectors

```bash
$ kubectl kgateway policy hybrid-policy -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/hybrid-policy ===

📊 Policy Status:
  ✅ 2 ancestor(s) found - Policy is ATTACHED
    1. Gateway kgateway-test/super-gateway
    2. Gateway kgateway-test/another-gateway

🎯 Target Reference Analysis:
  Target 1: HTTPRoute kgateway-test/specific-route
    ✅ Target exists
    📍 HTTPRoute has 1 parent reference(s)
      Parent 1: Gateway kgateway-test/super-gateway
        ✅ Gateway exists

🏷️ Target Selector Analysis:
  Selector 1: HTTPRoute with labels map[app:my-app version:v1]
    ✅ Found 3 matching target(s)
      1. app-route-v1
      2. app-route-canary
      3. app-route-fallback
    📍 HTTPRoute has 1 parent reference(s)
      Parent 1: Gateway kgateway-test/another-gateway
        ✅ Gateway exists
  Selector 2: Gateway with labels map[environment:production]
    ✅ Found 1 matching target(s)
      1. prod-gateway

🔍 Common Issues Check:
  ✅ No common issues detected
```

### Scenario 6: Policy with No Targets Specified

```bash
$ kubectl kgateway policy empty-policy -n kgateway-test

=== Diagnosing TrafficPolicy: kgateway-test/empty-policy ===

📊 Policy Status:
  ❌ No ancestors found - Policy is UNATTACHED

🎯 Target Reference Analysis:

🏷️ Target Selector Analysis:

🔍 Common Issues Check:
  ⚠️  Policy has no targetRefs or targetSelectors specified
```

## Integration with CI/CD

The kubectl plugin can be integrated into deployment pipelines for automated validation:

```yaml
# Example GitHub Actions step
- name: Validate TrafficPolicy Attachments
  run: |
    kubectl kgateway policy --all-namespaces
    if [ $? -ne 0 ]; then
      echo "❌ Policy attachment issues detected"
      exit 1
    fi
```

## Building from Source

For development or custom builds:

```bash
# Build for current OS
make kubectl-plugin

# Build for all platforms
make kubectl-plugin-all

# Use the binary directly
./_output/cmd/kubectl-kgateway/kubectl-kgateway-darwin-amd64 policy --help
```
