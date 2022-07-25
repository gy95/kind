# Integrate with [KubeEdge](https://github.com/kubeedge/kubeedge)

## Prerequisites
We need to put Kubernetes codes to `$GOPATH/k8s.io/kubernetes`, and put KubeEdge codes to `$GOPATH/github.com/kubeedge/kubeedge`.
And we need to ensure them checkout to the right release branch. For the former, we may need to run command like `git checkout v1.23.4`,
and for the latter, we may run the command like `git checkout v1.11.1`(For now, we may need some nits fix above the KubeEdge
release branch, ref: https://github.com/gy95/kubeedge/tree/kind).

When running `bin/kind build node-image` to build node-image image, kind will use the two source codes.

## How to build KubeEdge customized node-image
We need to checkout kind release tag, and add KubeEdge customized source codes(ref: https://github.com/gy95/kind/tree/release-v0.11)
And then execute the below commands
```shell
make
bin/kind build node-image
bin/kind create cluster --config example.yaml --image kindest/node:latest
```
and then you can see the contents as follows:
```shell
Creating cluster "kind" ...
 ✓ Ensuring node image (kindest/node:latest) 🖼
 ✓ Preparing nodes 📦 📦 📦  
 ✓ Writing configuration 📜 
 ✓ Starting control-plane 🕹️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️️ 
 ✓ Installing CNI 🔌 
 ✓ Installing StorageClass 💾 
 ✓ Joining worker nodes 🚜 
Set kubectl context to "kind-kind"
You can now use your cluster with:

kubectl cluster-info --context kind-kind

Have a nice day! 👋
 ✓ Starting KubeEdge 📜 
 ✓ Joining edge nodes 🚜
```
and you can see all the nodes are in `Ready` status, included `control-plane`, `worker` and `edge-node`(KubeEdge edgecore nodes).
```shell
# kubectl get node -owide
NAME                 STATUS   ROLES                  AGE   VERSION                                              INTERNAL-IP   EXTERNAL-IP   OS-IMAGE       KERNEL-VERSION       CONTAINER-RUNTIME
kind-control-plane   Ready    control-plane,master   71s   v1.23.4                                              172.18.0.2    <none>        Ubuntu 21.04   4.15.0-169-generic   containerd://1.5.2
kind-edge-node       Ready    agent,edge             49s   v1.22.6-kubeedge-v1.11.0-beta.0.102+138bc34e64e008   172.18.0.3    <none>        Ubuntu 21.04   4.15.0-169-generic   remote://1.5.2
kind-worker          Ready    <none>                 48s   v1.23.4                                              172.18.0.4    <none>        Ubuntu 21.04   4.15.0-169-generic   containerd://1.5.2
```