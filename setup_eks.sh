#!/bin/bash
# Lambda-G EKS Test Cluster Setup
# Creates a 3-node EKS cluster, runs benchmark, tears down
#
# COST: ~$0.50-1.00 for 1 hour of testing
# Region: ap-south-1 (Mumbai)
#
# Usage:
#   ./setup_eks.sh create    # Create cluster
#   ./setup_eks.sh test      # Run benchmark
#   ./setup_eks.sh destroy   # Tear down (IMPORTANT — stops billing!)

CLUSTER_NAME="lambda-g-test"
REGION="ap-south-1"
NODE_TYPE="t3.medium"  # 2 CPU, 4GB RAM — cheapest for testing
NODE_COUNT=3

set -e

case "$1" in
  create)
    echo "🚀 Creating EKS cluster: $CLUSTER_NAME"
    echo "   Region: $REGION"
    echo "   Nodes: $NODE_COUNT × $NODE_TYPE"
    echo "   Estimated cost: ~$0.30/hour"
    echo ""

    # Check auth
    aws sts get-caller-identity || { echo "❌ AWS not authenticated. Run: aws configure"; exit 1; }

    # Create cluster
    eksctl create cluster \
      --name "$CLUSTER_NAME" \
      --region "$REGION" \
      --node-type "$NODE_TYPE" \
      --nodes "$NODE_COUNT" \
      --nodes-min 3 \
      --nodes-max 3 \
      --managed \
      --with-oidc

    echo "✅ Cluster created. Verifying..."
    kubectl get nodes
    ;;

  test)
    echo "🧪 Running Lambda-G benchmark on EKS..."

    # Verify connection
    kubectl get nodes || { echo "❌ Not connected to cluster"; exit 1; }

    # Run the simulation benchmark (doesn't need the controller deployed)
    echo "Running simulation benchmark..."
    python3 benchmark_simulation.py | tee eks_benchmark_results.txt

    # Also run live benchmark (deploys real pods)
    echo ""
    echo "Running LIVE benchmark (real pods on EKS)..."
    python3 benchmark_full.py | tee eks_live_results.txt

    echo ""
    echo "✅ Results saved to eks_benchmark_results.txt and eks_live_results.txt"
    ;;

  deploy)
    echo "📦 Deploying Lambda-G controller to EKS..."

    # Build and push Docker image
    echo "Building Docker image..."
    docker buildx build --platform linux/amd64 -t bitsabhi/lambda-g-controller:latest --push .

    # Deploy with Helm
    echo "Installing Helm chart..."
    helm install lambda-g charts/lambda-g --namespace lambda-g --create-namespace

    echo "✅ Lambda-G deployed. Check: kubectl get pods -n lambda-g"
    ;;

  destroy)
    echo "💣 Destroying EKS cluster: $CLUSTER_NAME"
    echo "   This stops ALL billing for the cluster."
    read -p "   Are you sure? (yes/no): " confirm
    if [ "$confirm" = "yes" ]; then
      eksctl delete cluster --name "$CLUSTER_NAME" --region "$REGION"
      echo "✅ Cluster destroyed. No more charges."
    else
      echo "Cancelled."
    fi
    ;;

  status)
    echo "📊 Cluster status:"
    kubectl get nodes -o wide
    echo ""
    kubectl get pods --all-namespaces | head -20
    ;;

  *)
    echo "Lambda-G EKS Test Setup"
    echo ""
    echo "Usage: $0 {create|test|deploy|destroy|status}"
    echo ""
    echo "  create   — Create 3-node EKS cluster (~$0.30/hr)"
    echo "  test     — Run benchmark (simulation + live)"
    echo "  deploy   — Build Docker image + Helm install"
    echo "  destroy  — DELETE cluster (stops billing!)"
    echo "  status   — Show cluster state"
    ;;
esac
