# Standalone Testing Guide (Without OpenMCP Platform)

This guide shows how to test the service provider by deploying it directly to an MCP cluster, without using the OpenMCP platform infrastructure.

## Prerequisites

- A Kubernetes cluster (can be kind, minikube, or any K8s cluster)
- kubectl configured to access the cluster
- Docker for building the image

## Step 1: Create a Test Cluster

If you don't have a cluster, create one with kind:

```bash
kind create cluster --name test-mcp
kubectl config use-context kind-test-mcp
```

## Step 2: Build and Load the Image

```bash
# Build the service provider
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/service-provider-ksm ./cmd/service-provider-ksm

# Build Docker image
docker build -t service-provider-ksm:test .

# Load into kind cluster
kind load docker-image service-provider-ksm:test --name test-mcp
```

## Step 3: Deploy the Service Provider

```bash
# Deploy service provider (includes init job and controller)
kubectl apply -f test/standalone/deployment.yaml
```

## Step 4: Verify Deployment

Check that the init job completed:
```bash
kubectl get jobs -n service-provider-ksm
# Should show: service-provider-ksm-init   1/1           Xs        Xs
```

Check that CRDs are installed:
```bash
kubectl get crds | grep ksm
# Should show:
# kubestatemetrics.ksm.services.openmcp.cloud
# kubestatemetricsconfigs.ksm.services.openmcp.cloud
```

Check that the controller is running:
```bash
kubectl get pods -n service-provider-ksm
# Should show: service-provider-ksm-xxx   1/1     Running
```

Check controller logs:
```bash
kubectl logs -n service-provider-ksm -l app=service-provider-ksm --tail=50
```

You should see logs like:
```
"Service provider running on local MCP cluster, watching for KubeStateMetrics resources"
"starting manager"
```

## Step 5: Create Test Resources

Deploy the test KubeStateMetricsConfig and KubeStateMetrics:

```bash
kubectl apply -f test/e2e/mcp/kubestatemetrics.yaml
```

## Step 6: Verify Resources

Check that resources were created:
```bash
kubectl get kubestatemetricsconfig,kubestatemetrics
```

Expected output:
```
NAME                                                            CONFIGMAP                PHASE   AGE
kubestatemetricsconfig.ksm.services.openmcp.cloud/test-config   test-config-ksm-config   Ready   Xs

NAME                                                   PHASE   AGE
kubestatemetrics.ksm.services.openmcp.cloud/test-ksm   Ready   Xs
```

Check ConfigMap was created:
```bash
kubectl get configmap test-config-ksm-config -n default
```

Check kube-state-metrics deployment:
```bash
kubectl get all -n observability
```

You should see:
- Deployment: `kube-state-metrics`
- Service: `kube-state-metrics`
- Pod: `kube-state-metrics-xxx`
- ServiceAccount, ClusterRole, ClusterRoleBinding

## Step 7: Test Metrics

Port-forward to kube-state-metrics:
```bash
kubectl port-forward -n observability svc/kube-state-metrics 8080:8080
```

Fetch metrics (in another terminal):
```bash
curl http://localhost:8080/metrics | grep -E "kube_|crossplane_btp"
```

You should see metrics output!

## Step 8: Test Updates

### Update Configuration

```bash
kubectl edit kubestatemetricsconfig test-config
# Make a change to the config and save
```

Watch for ConfigMap update:
```bash
kubectl get configmap test-config-ksm-config -n default -o yaml
```

### Scale Deployment

```bash
kubectl patch kubestatemetrics test-ksm -p '{"spec":{"replicas":2}}' --type=merge
```

Verify scaling:
```bash
kubectl get deployment kube-state-metrics -n observability
# Should show READY: 2/2
```

## Step 9: Test Deletion

Delete KubeStateMetrics:
```bash
kubectl delete kubestatemetrics test-ksm
```

Verify cleanup:
```bash
kubectl get all -n observability
# Should show: No resources found
```

Delete KubeStateMetricsConfig:
```bash
kubectl delete kubestatemetricsconfig test-config
```

Verify ConfigMap is deleted:
```bash
kubectl get configmap test-config-ksm-config -n default
# Should show: Error from server (NotFound)
```

## Cleanup

Remove service provider:
```bash
kubectl delete -f test/standalone/deployment.yaml
```

Delete the cluster (if using kind):
```bash
kind delete cluster --name test-mcp
```

## Troubleshooting

### Init Job Failed

Check init job logs:
```bash
kubectl logs -n service-provider-ksm job/service-provider-ksm-init
```

Common issues:
- Image not loaded into cluster
- Permissions issues (check ServiceAccount/ClusterRole)

### Controller Not Starting

Check deployment:
```bash
kubectl describe deployment -n service-provider-ksm service-provider-ksm
```

Check pod logs:
```bash
kubectl logs -n service-provider-ksm -l app=service-provider-ksm
```

### Resources Not Deploying

Check controller logs for errors:
```bash
kubectl logs -n service-provider-ksm -l app=service-provider-ksm --tail=100
```

Check resource status:
```bash
kubectl get kubestatemetrics test-ksm -o yaml
```

Look at the `status` section for error messages.

## Advantages of Standalone Testing

1. **Faster**: No need to set up full OpenMCP platform
2. **Simpler**: Direct deployment, easier to debug
3. **Isolated**: Test just the service provider functionality
4. **Flexible**: Use any Kubernetes cluster

## Next Steps

After validating standalone deployment works, you can:
1. Test with OpenMCP platform integration
2. Test automatic deployment to multiple MCP clusters
3. Test with real workloads
