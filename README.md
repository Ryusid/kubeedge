# KubeEdge — Motion Detection & Classification at the Edge

This repository contains an end‑to‑end edge pipeline for **motion detection** and **object classification** integrated with **KubeEdge**. It implements and compares three protocol paths between a motion app (camera side) and edge services:

- **MQTT**: telemetry + images over MQTT  
- **CoAP**: telemetry + images over CoAP (with block‑wise transfer)  
- **Hybrid (final)**: **CoAP for telemetry** and **gRPC for images** to an edge classifier

> Why hybrid? CoAP/MQTT are great for telemetry, but gRPC is far more efficient and predictable for binary payloads (images). This repo validates that and adopts the hybrid as the final architecture.

---

## High‑level architecture

- **Cloud VM (Kubernetes control plane + CloudCore)**: manages device models/twins and syncs state with edge.  
- **Edge node (Raspberry Pi + EdgeCore)**: runs mappers and the classifier, and keeps device twins up‑to‑date even with flaky links.  
- **Motion app (off‑cluster)**: detects motion via OpenCV (MOG2), crops ROI, sends telemetry (CoAP or MQTT) and the ROI image (gRPC in the hybrid).

Design observations (from measurements in this project):

- Small payloads (~1–10 KB): CoAP/MQTT OK; gRPC still fastest.  
- Larger images (~150 KB): gRPC stays low‑latency; CoAP/MQTT increase more noticeably.  
- Conclusion: **Hybrid** preserves simple telemetry (CoAP) and efficient media transport (gRPC).

---



**Twin properties** are consistent across protocols: `motion` (bool), `last_detection` (string timestamp), `class` (string). DeviceModels/Devices bind those properties to MQTT topics or CoAP resources.

---

## Reproducibility

### Prerequisites

- A **Kubernetes** cluster (single-node control plane VM is fine) with `kubectl` access.  
- A **Raspberry Pi** (or similar) as the **edge node**.  
- **KubeEdge** binaries (`keadm`) on cloud & edge.  
- Container runtime on the edge (**containerd**).  
- Optional: a host (Windows/Linux or Smart camera) for the motion app.

> The flow uses **CloudCore** (in the cluster) + **EdgeCore** (on the Pi).

---

### 1) Install KubeEdge — Cloud (CloudCore)

On the **cloud VM** (where the Kubernetes control plane runs):

```bash
# Install keadm appropriate for your OS, then:
sudo keadm init \
  --advertise-address="<CLOUD_IP>" \
  --kubeedge-version="v1.20.0" \
  --kube-config="$HOME/.kube/config"

# Get the join token for the edge:
keadm gettoken
```

This deploys **CloudCore** in namespace `kubeedge` and prints a token for the edge join.

---

### 2) Join the edge node (EdgeCore)

On the **Raspberry Pi**:

```bash
sudo keadm join \
  --cloudcore-ipport="<CLOUD_IP>:10000" \
  --token="<TOKEN_FROM_ABOVE>" \
  --kubeedge-version="v1.20.0"
```

Once joined, your Pi appears as a Kubernetes node (e.g., `raspberrypi`).

> Make sure Cloud ↔ Edge can reach each other on CloudCore ports (10000/10001 for CloudHub, 10003/10004 for logs/exec via CloudStream/Tunnel).


Note : see the [KubeEdge docs](https://kubeedge.io/docs/category/setup) for detailed setup instructions.
---

### 3) Choose a path (MQTT / CoAP / Hybrid)

#### A) Hybrid (recommended): CoAP telemetry + gRPC images

On the **cloud** (with `kubectl`):

```bash
# Device model & instance for CoAP telemetry
kubectl apply -f grpc/coap-model.yaml
kubectl apply -f grpc/coap-instance.yaml
```

This defines the **motion / last_detection / class** properties and points the mapper at the motion app’s CoAP server (IP:port, resource paths).

#### B) MQTT variant

```bash
kubectl apply -f mqtt/mqtt-model.yaml
kubectl apply -f mqtt/mqtt-instance.yaml
```

#### C) CoAP-only variant (images over CoAP)

```bash
kubectl apply -f coap/coap-model.yaml
kubectl apply -f coap/coap-instance.yaml
```

---

### 4) Run edge components

#### Hybrid (CoAP mapper + gRPC classifier)

- **CoAP mapper** (edge pod/daemonset; adjust to your environment):

```bash
kubectl apply -f grpc/coap-mapper/resource/deployment.yaml
```

- **gRPC classifier** (edge worker):

```bash
kubectl apply -f grpc/classifier/deployment.yaml
# Ensure the worker binds 0.0.0.0:50051 (hostNetwork: true is the simplest for testing).
```

> The motion app will call the classifier directly via **gRPC** and publish telemetry via **CoAP**; the CoAP mapper updates the device twin accordingly.

#### MQTT or pure CoAP variants

Use the corresponding manifests in `mqtt/` or `coap/` to deploy a protocol‑specific classifier and the mapper.

---

### 5) Run the motion app (off‑cluster)

- **Hybrid**: start the motion app in `grpc/motiondetection/run.py`, set:
  - `GRPC_ADDR=<EDGE_NODE_IP>:50051` (classifier address)
  - exposes CoAP resources for telemetry (make sure the coap-instance uses the correct paths and address).

- **MQTT**: run `mqtt/mqtt-motion-detector/app.py` (make sure topics in the app.py match the ones in `mqtt-instance.yaml`).  
- **CoAP**: run `coap/coap-motion-detector/run.py` 
When motion is detected, the app crops an ROI and:
1) updates **motion**/**last_detection** via the telemetry protocol,  
2) sends the ROI image to the classifier (gRPC in hybrid),  
3) classifier writes back **class** (exposed via the telemetry protocol),  
4) mapper reports properties to the Device Twin on the next collect/report cycles.

---

## Verification & troubleshooting

- **Pods on edge**:
  ```bash
  kubectl get pods -o wide -n default
  ```
- **Twin values**: use `kubectl get device <device-name> -o yaml -w` to see `motion`, `last_detection`, `class` updates in real time.
- **Logs/exec to edge pods** require CloudStream/Tunnel (10003/10004). If `kubectl logs` fails, verify tunnel ports and `edgeStream`/`cloudStream` settings, and read logs directly on the edge with `crictl logs` while fixing the tunnel.