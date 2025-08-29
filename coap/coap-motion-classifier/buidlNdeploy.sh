#!/bin/bash

kubectl delete -f Deployment.yaml

sudo docker buildx build --platform linux/arm64,linux/amd64 -t ryusid/coap-classifier:latest . --push

kubectl apply -f  Deployment.yaml
