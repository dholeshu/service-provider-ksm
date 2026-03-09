# Architecture Redesign: Service Provider on MCP Clusters

## Overview

The service provider has been redesigned to run **ON MCP clusters** instead of running on the platform cluster and watching the onboarding cluster.

## Key Changes

### 1. Deployment Location
- **Before**: Service provider runs on platform cluster
- **After**: Service provider runs on each MCP cluster (deployed by OpenMCP operator)

### 2. Resource Location
- **Before**: Users create resources on onboarding cluster
- **After**: Users create resources on their MCP cluster

### 3. Watch Pattern
- **Before**: Cross-cluster watching (platform → onboarding → deploy to MCP)
- **After**: Local watching (MCP → deploy to same MCP)

### 4. CRD Installation
- **Before**: CRDs installed on platform and onboarding clusters
- **After**: CRDs installed on each MCP cluster during init

## Files Modified

### cmd/service-provider-ksm/main.go
- Removed onboarding cluster initialization
- Removed platform cluster scheme (now just localMCPScheme)
- Init job now installs CRDs locally on MCP cluster
- Manager watches local MCP cluster (not remote onboarding)
- Removed ClusterAccess complexity for watching
- Simplified to single-cluster operation

### internal/controller/kubestatemetrics_controller.go
- Changed `OnboardingCluster` → `LocalMCPCluster`
- Removed `PlatformCluster` field
- Fetches KubeStateMetricsConfig from local cluster
- Deploys to local cluster (already was using MCPCluster)

### internal/controller/kubestatemetricsconfig_controller.go
- Changed `OnboardingCluster` → `LocalMCPCluster`
- Removed `PlatformCluster` field
- Creates ConfigMap on local cluster

### api/crds/manifests/*.yaml
- Updated labels from `onboarding` to `mcp`
- ProviderConfig remains on `platform`

### test/e2e/mcp/kubestatemetrics.yaml
- New test file for MCP-based testing
- Replaces onboarding-based test file

### MANUAL_TEST.md
- Completely rewritten for new architecture
- Documents MCP-local workflow
- Explains architectural differences

## Benefits

1. **Simpler**: No cross-cluster watching, no ClusterAccess complexity
2. **Faster**: Local operations only
3. **User-friendly**: Users only need MCP cluster access
4. **Standard**: Follows normal Kubernetes controller pattern
5. **Scalable**: Each MCP cluster is independent

## Migration Notes

- Old onboarding-based deployments will not work
- ServiceProvider must be configured to deploy to MCP clusters
- Users need to create resources on MCP clusters, not onboarding
- No backward compatibility with old architecture

## Testing

Use `MANUAL_TEST.md` for step-by-step testing instructions.

Key test flow:
1. Create MCP cluster
2. Deploy ServiceProvider (OpenMCP deploys to MCP)
3. Create resources on MCP cluster
4. Verify local deployment

## Future Work

- Verify ServiceProvider spec for MCP deployment mode
- Test with multiple MCP clusters
- Update any CI/CD pipelines
- Update user documentation
