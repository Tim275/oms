# OMS Deployment Guide - GitHub Container Registry + Kubernetes

**Production-Ready Deployment mit GitHub Actions & ghcr.io**

---

## 🎯 Deployment Flow

```
┌────────────────────────────────────────────────────────────────────┐
│  1. Push Code   →   2. GitHub Actions   →   3. ghcr.io   →   4. K8s │
│  (git push)     →   (Build Images)      →   (Registry)   →   (Deploy)│
└────────────────────────────────────────────────────────────────────┘
```

---

## 📦 Step 1: GitHub Setup

### 1.1 Push Code to GitHub

```bash
cd /Users/timour/Desktop/Golang/order-microservices

# Initialize git (if not already done)
git init
git add .
git commit -m "feat: Production-ready OMS with health checks and security hardening"

# Add remote (replace with your repo)
git remote add origin https://github.com/YOUR_USERNAME/order-microservices.git
git push -u origin main
```

### 1.2 Enable GitHub Actions

1. Go to your repository on GitHub
2. Click **Actions** tab
3. GitHub Actions should automatically detect `.github/workflows/build-and-push.yml`
4. The workflow will automatically run on push to `main`

### 1.3 Verify Images Published

After the workflow completes:

1. Go to your GitHub repository
2. Click **Packages** on the right side
3. You should see 7 packages:
   - `oms-gateway`
   - `oms-orders`
   - `oms-payments`
   - `oms-stock`
   - `oms-kitchen`
   - `oms-customer-app`
   - `oms-kitchen-display`

---

## 🔐 Step 2: Configure Image Pull Access

### Option A: Public Images (Easiest)

**Make packages public:**

1. Go to each package (e.g., `ghcr.io/YOUR_USERNAME/oms-gateway`)
2. Click **Package settings**
3. Scroll to **Danger Zone**
4. Click **Change visibility** → **Public**
5. Repeat for all 7 packages

**No imagePullSecrets needed!** ✅

### Option B: Private Images (More Secure)

**Create GitHub Personal Access Token:**

1. GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Generate new token (classic)
3. Select scopes: `read:packages`
4. Copy the token (starts with `ghp_...`)

**Create Kubernetes Secret:**

```bash
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=YOUR_GITHUB_USERNAME \
  --docker-password=ghp_YOUR_TOKEN \
  --namespace=oms
```

**Add imagePullSecrets to deployments:**

```yaml
# overlays/production/imagepullsecret-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway
spec:
  template:
    spec:
      imagePullSecrets:
      - name: ghcr-secret
---
# Repeat for all deployments...
```

---

## 🚀 Step 3: Update Image References

**Edit:** `/Users/timour/Desktop/Golang/oms-k8s/overlays/production/image-patch.yaml`

**Replace `YOUR_USERNAME` with your actual GitHub username:**

```bash
cd /Users/timour/Desktop/Golang/oms-k8s

# Quick replace (Mac)
sed -i '' 's/YOUR_USERNAME/your-actual-username/g' overlays/production/image-patch.yaml

# OR manually edit the file
```

**Example:**
```yaml
# Before:
image: ghcr.io/YOUR_USERNAME/oms-gateway:latest

# After:
image: ghcr.io/timour/oms-gateway:latest
```

---

## 🎯 Step 4: Deploy to Kubernetes

### 4.1 Deploy to Production

```bash
cd /Users/timour/Desktop/Golang/oms-k8s

# Deploy everything
kubectl apply -k overlays/production/

# Watch pods start
kubectl get pods -n oms -w
```

### 4.2 Verify Deployment

```bash
# Check all pods running
kubectl get pods -n oms

# Expected output:
# NAME                              READY   STATUS    RESTARTS   AGE
# gateway-xxx-xxx                   2/2     Running   0          2m
# orders-xxx-xxx                    2/2     Running   0          2m
# payments-xxx-xxx                  2/2     Running   0          2m
# stock-xxx-xxx                     2/2     Running   0          2m
# kitchen-xxx-xxx                   2/2     Running   0          2m
# customer-app-xxx-xxx              2/2     Running   0          2m
# kitchen-display-xxx-xxx           2/2     Running   0          2m
```

### 4.3 Check Health Endpoints

```bash
# Port forward gateway
kubectl port-forward -n oms svc/gateway 8080:8080 &

# Test health
curl http://localhost:8080/health
# → {"status":"healthy","service":"gateway"}

# Test API
curl http://localhost:8080/api/menu
# → [Stripe products...]
```

### 4.4 Verify HPAs

```bash
kubectl get hpa -n oms

# NAME       REFERENCE            TARGETS         MINPODS   MAXPODS   REPLICAS
# gateway    Deployment/gateway   cpu: 15%/70%    2         5         2
# orders     Deployment/orders    cpu: 10%/70%    2         4         2
# payments   Deployment/payments  cpu: 8%/70%     2         3         2
# stock      Deployment/stock     cpu: 12%/70%    2         3         2
```

---

## 🔄 Step 5: Continuous Deployment

### Automatic Rebuilds

Every time you push code to `main`, GitHub Actions will:

1. ✅ Build all Docker images
2. ✅ Push to `ghcr.io/YOUR_USERNAME/oms-*:latest`
3. ✅ Tag with commit SHA

### Manual Redeploy

```bash
# Force pull latest images
kubectl rollout restart deployment -n oms gateway
kubectl rollout restart deployment -n oms orders
kubectl rollout restart deployment -n oms payments
kubectl rollout restart deployment -n oms stock
kubectl rollout restart deployment -n oms kitchen

# Or restart all at once
kubectl rollout restart deployment -n oms
```

### Use Specific Image Tags

Instead of `:latest`, use commit SHAs for reproducibility:

```yaml
# image-patch.yaml
image: ghcr.io/timour/oms-gateway:main-a1b2c3d  # Commit SHA tag
```

---

## 🎨 Advanced: ArgoCD GitOps (Optional)

### Install ArgoCD

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
```

### Create ArgoCD Application

```yaml
# overlays/production/argocd-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: oms
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/YOUR_USERNAME/oms-k8s.git
    targetRevision: main
    path: overlays/production
  destination:
    server: https://kubernetes.default.svc
    namespace: oms
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true
```

Deploy:
```bash
kubectl apply -f overlays/production/argocd-application.yaml
```

Now ArgoCD will automatically sync your cluster with Git! 🚀

---

## 📊 Monitoring

### Check Prometheus ServiceMonitors

```bash
kubectl get servicemonitor -n oms

# NAME              AGE
# gateway-metrics   5m
# orders-metrics    5m
```

### Access Prometheus Metrics

```bash
# Port forward orders metrics
kubectl port-forward -n oms svc/orders 9001:9001 &
curl http://localhost:9001/metrics
```

---

## 🐛 Troubleshooting

### Images Not Pulling

**Problem:** `ErrImagePull` or `ImagePullBackOff`

**Solution:**

1. Check image exists:
```bash
docker pull ghcr.io/YOUR_USERNAME/oms-gateway:latest
```

2. If private, verify imagePullSecret:
```bash
kubectl get secret ghcr-secret -n oms
```

3. Check logs:
```bash
kubectl describe pod -n oms gateway-xxx-xxx
```

### Health Probes Failing

**Problem:** Pods restarting due to failed health checks

**Solution:**

1. Check health endpoint:
```bash
kubectl port-forward -n oms <pod-name> 8080:8080
curl http://localhost:8080/health
```

2. Check logs:
```bash
kubectl logs -n oms <pod-name>
```

3. Increase `initialDelaySeconds` if app needs more startup time

### HPA Not Scaling

**Problem:** HPA shows `<unknown>` for CPU/Memory

**Solution:**

1. Check Metrics Server installed:
```bash
kubectl get deployment -n kube-system metrics-server
```

2. Check resource requests defined:
```bash
kubectl describe hpa -n oms gateway
```

---

## 📝 Summary

### ✅ What You Have Now

| Feature | Status |
|---------|--------|
| **CI/CD Pipeline** | ✅ GitHub Actions builds on push |
| **Container Registry** | ✅ ghcr.io (free, unlimited) |
| **Auto-Scaling** | ✅ HPAs configured |
| **High Availability** | ✅ PDBs protect from disruption |
| **Security** | ✅ Non-root, read-only filesystem |
| **Health Checks** | ✅ Liveness + Readiness probes |
| **Monitoring** | ✅ Prometheus ServiceMonitors |
| **Multi-Arch** | ✅ amd64 + arm64 support |

### 🎯 Next Steps

1. **Push to GitHub** → Triggers automatic build
2. **Make packages public** OR create imagePullSecret
3. **Update image references** with your GitHub username
4. **Deploy to Kubernetes** → `kubectl apply -k overlays/production/`
5. **Verify everything works** → Health checks, HPAs, metrics

---

## 🎉 You're Production-Ready!

Your OMS is now deployed following Google/AWS/Netflix best practices with:
- Zero-downtime deployments
- Automatic scaling
- Self-healing capabilities
- Enterprise-grade security
- Observable and monitored

**Happy deploying! 🚀**
