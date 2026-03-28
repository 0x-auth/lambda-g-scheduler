#!/bin/bash

echo "📊 Starting Lambda-G Benchmark..."

# 1. Clear existing pods
kubectl delete pod -l app=benchmark --now

echo "Step 1: Running Legacy Baseline (Standard Scheduling)..."
for i in {1..10}; do
  kubectl run legacy-$i --image=nginx --labels="app=benchmark,type=legacy" --requests='cpu=800m,memory=128Mi'
done
sleep 5
python coherence_engine/auditor/auditor.py > baseline_report.txt

echo "Step 2: Cleaning up baseline..."
kubectl delete pod -l type=legacy --now

echo "Step 3: Running Lambda-G Coherence Packing..."
# Note: Ensure your controller is running in another tab!
for i in {1..10}; do
  cat <<YAML | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: lambdag-$i
  labels:
    app: benchmark
    type: lambdag
  annotations:
    schedulerName: "lambda-g"
spec:
  schedulerName: lambda-g-manual
  containers:
  - name: nginx
    image: nginx
    resources:
      requests:
        cpu: "800m"
        memory: "128Mi"
YAML
done

sleep 5
python coherence_engine/auditor/auditor.py > lambdag_report.txt

echo "✅ Benchmark Complete!"
echo "Check baseline_report.txt vs lambdag_report.txt to see the 'Stranded' delta."
