# Testing Guide for KubeStateMetrics Service Provider

## Prerequisites

- Docker installed
- kind installed
- kubectl installed
- Project built: `go build -o bin/service-provider-ksm ./cmd/service-provider-ksm`

## Step 1: Create a Kind Cluster

```bash
# Create a kind cluster
kind create cluster --name service-provider-ksm

# Export kubeconfig
kind export kubeconfig --name service-provider-ksm

# Verify cluster is ready
kubectl cluster-info
kubectl get nodes
```

## Step 2: Install CRDs

```bash
# Install all CRDs
kubectl apply -f api/crds/manifests/

# Verify CRDs are installed
kubectl get crd | grep ksm
```

Expected output:
```
kubestatemetrics.ksm.services.openmcp.cloud          2026-03-06T14:32:13Z
kubestatemetricsconfigs.ksm.services.openmcp.cloud   2026-03-06T14:32:13Z
providerconfigs.ksm.services.openmcp.cloud           2026-03-06T14:32:13Z
```

## Step 3: Create Namespace

```bash
kubectl create namespace observability
```

## Step 4: Create Test Configuration

### Option A: Custom Resources Only

Create `test-custom-only.yaml`:

```yaml
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: custom-only-config
  namespace: observability
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: "apps"
            version: "v1"
            kind: "Deployment"
          labelsFromPath:
            name: [metadata, name]
            namespace: [metadata, namespace]
          metricNamePrefix: test_deployment
          metrics:
            - name: replicas
              help: "Number of desired replicas for Deployment"
              each:
                type: Gauge
                gauge:
                  path: [spec, replicas]
            - name: available_replicas
              help: "Number of available replicas"
              each:
                type: Gauge
                gauge:
                  path: [status, availableReplicas]
```

Apply it:
```bash
kubectl apply -f test-custom-only.yaml

# Verify config created
kubectl get kubestatemetricsconfig -n observability
```

### Option B: Use Existing Examples

```bash
# Use the provided example
kubectl apply -f examples/with-config-ref.yaml
```

## Step 5: Manually Create Resources (Simulating Controller)

Since the full OpenMCP controller requires an OpenMCP environment, we'll manually create the resources that the controller would create.

### 5.1: Create ConfigMap

```bash
# Extract config from KubeStateMetricsConfig
CONFIG_NAME="custom-only-config"  # or the name you used

kubectl create configmap ${CONFIG_NAME}-ksm-config -n observability \
  --from-literal=custom-resource-state-config.yaml="$(kubectl get kubestatemetricsconfig ${CONFIG_NAME} -n observability -o jsonpath='{.spec.customResourceStateConfig}')"

# Add labels
kubectl label configmap ${CONFIG_NAME}-ksm-config -n observability \
  app.kubernetes.io/name=kube-state-metrics \
  app.kubernetes.io/component=config \
  app.kubernetes.io/managed-by=service-provider-ksm

# Verify ConfigMap
kubectl get configmap ${CONFIG_NAME}-ksm-config -n observability -o yaml
```

### 5.2: Create RBAC Resources

```bash
# Create ServiceAccount
kubectl create serviceaccount kube-state-metrics -n observability

# Create ClusterRole
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-state-metrics
  labels:
    app.kubernetes.io/name: kube-state-metrics
    app.kubernetes.io/component: exporter
rules:
  - apiGroups: ["authentication.k8s.io"]
    resources: ["tokenreviews"]
    verbs: ["create"]
  - apiGroups: ["authorization.k8s.io"]
    resources: ["subjectaccessreviews"]
    verbs: ["create"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
EOF

# Create ClusterRoleBinding
kubectl create clusterrolebinding kube-state-metrics \
  --clusterrole=kube-state-metrics \
  --serviceaccount=observability:kube-state-metrics
```

### 5.3: Create Deployment

```bash
CONFIG_NAME="custom-only-config"  # or the name you used

kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-state-metrics
  namespace: observability
  labels:
    app.kubernetes.io/name: kube-state-metrics
    app.kubernetes.io/component: exporter
    app.kubernetes.io/version: v2.13.0
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: kube-state-metrics
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kube-state-metrics
        app.kubernetes.io/component: exporter
        app.kubernetes.io/version: v2.13.0
    spec:
      serviceAccountName: kube-state-metrics
      automountServiceAccountToken: true
      containers:
        - name: kube-state-metrics
          image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
          args:
            - --custom-resource-state-only=true
            - --custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml
          ports:
            - containerPort: 8080
              name: http-metrics
            - containerPort: 8081
              name: telemetry
          livenessProbe:
            httpGet:
              path: /livez
              port: http-metrics
            initialDelaySeconds: 5
            timeoutSeconds: 5
          readinessProbe:
            httpGet:
              path: /readyz
              port: telemetry
            initialDelaySeconds: 5
            timeoutSeconds: 5
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65534
            seccompProfile:
              type: RuntimeDefault
          volumeMounts:
            - name: config
              mountPath: /etc/kube-state-metrics/
      nodeSelector:
        kubernetes.io/os: linux
      volumes:
        - name: config
          configMap:
            name: ${CONFIG_NAME}-ksm-config
EOF
```

### 5.4: Create Service

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: kube-state-metrics
  namespace: observability
  labels:
    app.kubernetes.io/name: kube-state-metrics
    app.kubernetes.io/component: exporter
spec:
  clusterIP: None
  ports:
    - name: http-metrics
      port: 8080
      targetPort: http-metrics
    - name: telemetry
      port: 8081
      targetPort: telemetry
  selector:
    app.kubernetes.io/name: kube-state-metrics
EOF
```

## Step 6: Verify Deployment

### Check Rollout Status

```bash
kubectl rollout status deployment/kube-state-metrics -n observability --timeout=120s
```

### Check Pod Status

```bash
kubectl get pods -n observability
```

Expected output:
```
NAME                                  READY   STATUS    RESTARTS   AGE
kube-state-metrics-xxxxxxxxxx-xxxxx   1/1     Running   0          1m
```

### Check Logs

```bash
kubectl logs -n observability deployment/kube-state-metrics --tail=30
```

Look for:
- ✅ "Starting kube-state-metrics"
- ✅ "Used CRD resources only" (if using --custom-resource-state-only)
- ✅ "Started metrics server"
- ✅ No errors

### Check Container Arguments

```bash
kubectl get deployment kube-state-metrics -n observability -o yaml | grep -A 5 "args:"
```

Verify the correct arguments are present:
- `--custom-resource-state-only=true`
- `--custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml`

## Step 7: Test Metrics (Optional)

### Option A: Port Forward

```bash
# In one terminal
kubectl port-forward -n observability deployment/kube-state-metrics 8080:8080

# In another terminal
curl http://localhost:8080/metrics | grep "^test_"
```

### Option B: Create a Test Pod

```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -s http://kube-state-metrics.observability:8080/metrics
```

## Step 8: Test with Deployments

Create some test deployments to see metrics:

```bash
# Create a test deployment
kubectl create deployment nginx --image=nginx --replicas=3

# Wait for it to be ready
kubectl rollout status deployment/nginx

# Check if KSM is collecting metrics for it
kubectl exec -n observability deployment/kube-state-metrics -- \
  wget -qO- http://localhost:8080/metrics 2>&1 | grep "test_deployment"
```

## Step 9: Cleanup

### Remove Test Resources

```bash
# Delete test deployment
kubectl delete deployment nginx --ignore-not-found

# Delete KSM resources
kubectl delete deployment kube-state-metrics -n observability
kubectl delete service kube-state-metrics -n observability --ignore-not-found
kubectl delete serviceaccount kube-state-metrics -n observability
kubectl delete clusterrole kube-state-metrics
kubectl delete clusterrolebinding kube-state-metrics

# Delete ConfigMaps
kubectl delete configmap -n observability -l app.kubernetes.io/name=kube-state-metrics

# Delete KubeStateMetricsConfig
kubectl delete kubestatemetricsconfig --all -n observability

# Delete namespace
kubectl delete namespace observability
```

### Remove CRDs

```bash
kubectl delete crd kubestatemetrics.ksm.services.openmcp.cloud \
  kubestatemetricsconfigs.ksm.services.openmcp.cloud \
  providerconfigs.ksm.services.openmcp.cloud
```

### Delete Kind Cluster

```bash
kind delete cluster --name service-provider-ksm
```

## Automated Test Script

Save this as `test-ksm-provider.sh`:

```bash
#!/bin/bash
set -e

echo "=== KubeStateMetrics Service Provider Test ==="

# Configuration
CONFIG_NAME="test-config"
NAMESPACE="observability"

echo "1. Installing CRDs..."
kubectl apply -f api/crds/manifests/

echo "2. Creating namespace..."
kubectl create namespace ${NAMESPACE}

echo "3. Creating KubeStateMetricsConfig..."
cat <<EOF | kubectl apply -f -
apiVersion: ksm.services.openmcp.cloud/v1alpha1
kind: KubeStateMetricsConfig
metadata:
  name: ${CONFIG_NAME}
  namespace: ${NAMESPACE}
spec:
  customResourceStateConfig: |
    kind: CustomResourceStateMetrics
    spec:
      resources:
        - groupVersionKind:
            group: "apps"
            version: "v1"
            kind: "Deployment"
          labelsFromPath:
            name: [metadata, name]
            namespace: [metadata, namespace]
          metricNamePrefix: test_deployment
          metrics:
            - name: replicas
              help: "Number of desired replicas"
              each:
                type: Gauge
                gauge:
                  path: [spec, replicas]
EOF

echo "4. Creating ConfigMap..."
kubectl create configmap ${CONFIG_NAME}-ksm-config -n ${NAMESPACE} \
  --from-literal=custom-resource-state-config.yaml="$(kubectl get kubestatemetricsconfig ${CONFIG_NAME} -n ${NAMESPACE} -o jsonpath='{.spec.customResourceStateConfig}')"

kubectl label configmap ${CONFIG_NAME}-ksm-config -n ${NAMESPACE} \
  app.kubernetes.io/name=kube-state-metrics \
  app.kubernetes.io/component=config

echo "5. Creating RBAC..."
kubectl create serviceaccount kube-state-metrics -n ${NAMESPACE}

kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-state-metrics
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
EOF

kubectl create clusterrolebinding kube-state-metrics \
  --clusterrole=kube-state-metrics \
  --serviceaccount=${NAMESPACE}:kube-state-metrics

echo "6. Creating Deployment..."
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-state-metrics
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kube-state-metrics
  template:
    metadata:
      labels:
        app: kube-state-metrics
    spec:
      serviceAccountName: kube-state-metrics
      containers:
        - name: kube-state-metrics
          image: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.13.0
          args:
            - --custom-resource-state-only=true
            - --custom-resource-state-config-file=/etc/kube-state-metrics/custom-resource-state-config.yaml
          ports:
            - containerPort: 8080
              name: http-metrics
          volumeMounts:
            - name: config
              mountPath: /etc/kube-state-metrics/
          securityContext:
            runAsUser: 65534
            runAsNonRoot: true
            readOnlyRootFilesystem: true
      volumes:
        - name: config
          configMap:
            name: ${CONFIG_NAME}-ksm-config
EOF

echo "7. Waiting for deployment..."
kubectl rollout status deployment/kube-state-metrics -n ${NAMESPACE} --timeout=120s

echo "8. Checking pod status..."
kubectl get pods -n ${NAMESPACE}

echo "9. Checking logs..."
kubectl logs -n ${NAMESPACE} deployment/kube-state-metrics --tail=20

echo ""
echo "=== Test Complete ==="
echo "To cleanup: kubectl delete namespace ${NAMESPACE} && kubectl delete crd -l openmcp.cloud/cluster=onboarding"
```

Make it executable and run:

```bash
chmod +x test-ksm-provider.sh
./test-ksm-provider.sh
```

## Troubleshooting

### Pod Not Starting

```bash
# Check pod events
kubectl describe pod -n observability -l app.kubernetes.io/name=kube-state-metrics

# Check pod logs
kubectl logs -n observability -l app.kubernetes.io/name=kube-state-metrics
```

### ConfigMap Issues

```bash
# Verify ConfigMap content
kubectl get configmap -n observability -o yaml

# Check if file exists in pod
kubectl exec -n observability deployment/kube-state-metrics -- ls -la /etc/kube-state-metrics/
```

### RBAC Issues

```bash
# Check if ServiceAccount exists
kubectl get sa -n observability

# Check ClusterRole
kubectl get clusterrole kube-state-metrics

# Check ClusterRoleBinding
kubectl get clusterrolebinding kube-state-metrics
```

## Expected Results

- ✅ CRDs installed successfully
- ✅ KubeStateMetricsConfig resource created
- ✅ ConfigMap created with correct content
- ✅ Deployment rollout successful
- ✅ Pod in Running state
- ✅ Logs show "Used CRD resources only"
- ✅ Metrics server started on port 8080
- ✅ No errors in logs

## Notes

- This manual test simulates what the OpenMCP service provider controller would do automatically
- The full automated reconciliation requires an OpenMCP environment
- The test confirms that the API structure and resource templates are correct
- Custom resource monitoring is working when you see "Used CRD resources only" in logs
