/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kubeedge implements the kubeedge action
package kubeedge

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/kind/pkg/cluster/constants"
	"sigs.k8s.io/kind/pkg/cluster/internal/create/actions"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
	"sigs.k8s.io/kind/pkg/log"
)

// controlPlaneIP is IP address that edgecore register to
var controlPlaneIP = ""

// KubeEdgeToken is token that edgecore used to register to cloudcore
var KubeEdgeToken string

// Action implements action for creating the node config files
type Action struct {
	AdvertiseAddress string
	ContainerMode    bool
}

// NewAction returns a new action for creating the config files
func NewAction(address string, containerMode bool) actions.Action {
	return &Action{
		AdvertiseAddress: address,
		ContainerMode:    containerMode,
	}
}

// Execute runs the action
func (a *Action) Execute(ctx *actions.ActionContext) error {
	ctx.Status.Start("Starting KubeEdge 📜")
	defer ctx.Status.End(false)

	if err := a.preProcess(ctx); err != nil {
		return fmt.Errorf("failed do pre process: %v", err)
	}

	// How to start cloudcore and edgecore localhost
	// The below logic is from kubeedge hack/local-up-kubeedge.sh
	// or from `keadm init/join` logic

	// bootstrap cloudcore: this operation should be on control-plane
	if a.ContainerMode {
		if err := a.BootstrapCloudCoreWithKeadm(ctx); err != nil {
			return fmt.Errorf("failed to bootstrap CloudCore with keadm: %v", err)
		}
	} else {
		if err := a.BootstrapCloudCore(ctx); err != nil {
			return fmt.Errorf("failed to bootstrap CloudCore: %v", err)
		}
	}

	// bootstrap edgecore: this operation should be on edge-node
	if a.ContainerMode {
		// now we only support bootstrap using binary
		if err := a.BootstrapEdgecore(ctx); err != nil {
			return err
		}
		// TODO: debug why edgecore segmentfault with nothing
		//if err := a.BootstrapEdgecoreWithKeadm(ctx); err != nil {
		//	return err
		//}
	} else {
		if err := a.BootstrapEdgecore(ctx); err != nil {
			return err
		}
	}

	// mark success
	ctx.Status.End(true)
	return nil
}

var patch = `"spec": {
	"template": {
		"spec": {
			"affinity": {
				"nodeAffinity": {
					"requiredDuringSchedulingIgnoredDuringExecution": {
						"nodeSelectorTerms": [
							{
								"matchExpressions": [
									{
										"key": "node-role.kubernetes.io/edge",
										"operator": "DoesNotExist"
									}
								]
							}
						]
					}
				}
			},
		}
	},
}`

// this patch cmd is from the above json
var kubeProxyNotScheduleOnEdgeNode string = `kubectl patch daemonset kube-proxy -n kube-system -p '{"spec": {"template": {"spec": {"affinity": {"nodeAffinity": {"requiredDuringSchedulingIgnoredDuringExecution": {"nodeSelectorTerms": [{"matchExpressions": [{"key": "node-role.kubernetes.io/edge", "operator": "DoesNotExist"}]}]}}}}}}}'`
var kindnetNotScheduleOnEdgeNode string = `kubectl patch daemonset kindnet -n kube-system -p '{"spec": {"template": {"spec": {"affinity": {"nodeAffinity": {"requiredDuringSchedulingIgnoredDuringExecution": {"nodeSelectorTerms": [{"matchExpressions": [{"key": "node-role.kubernetes.io/edge", "operator": "DoesNotExist"}]}]}}}}}}}'`

func (a *Action) preProcess(ctx *actions.ActionContext) error {
	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	// get the target node for this task
	// TODO: eliminate the concept of bootstrapcontrolplane node entirely
	// outside this method
	node, err := nodeutils.BootstrapControlPlaneNode(allNodes)
	if err != nil {
		return err
	}

	// check control plane ready
	name := ctx.Config.Name
	nodeName := fmt.Sprintf("node/%s-control-plane", name)
	cmd := node.Command("kubectl", "wait", "--for=condition=Ready", nodeName, "--timeout=180s")
	lines, err := exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to wait the control-plane ready %v ", lines))
	}

	//cmd = node.Command("bash", "-c", "kubectl delete daemonset kindnet -nkube-system")
	//lines, err = exec.CombinedOutputLines(cmd)
	//ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	//if err != nil {
	//	return errors.Wrap(err, fmt.Sprintf("failed to delete CNI %v ", lines))
	//}

	cmd = node.Command("bash", "-c", kindnetNotScheduleOnEdgeNode)
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to stop kindnet scheduled to edge nodes %v ", lines))
	}

	// edge-node not schedule kube-proxy
	cmd = node.Command("bash", "-c", kubeProxyNotScheduleOnEdgeNode)
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to stop daemonset kube-proxy scheduled to the edge nodes %v ", lines))
	}
	return nil
}

func (a *Action) BootstrapCloudCoreWithKeadm(ctx *actions.ActionContext) error {
	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	// get the target node for this task
	// TODO: eliminate the concept of bootstrapcontrolplane node entirely
	// outside this method
	node, err := nodeutils.BootstrapControlPlaneNode(allNodes)
	if err != nil {
		return err
	}

	// master nodes support running cloudcore
	// TODO: if we can run edge node successfully without running config and kubeadm join first, we can remove this operation
	// TODO: due to cloudcore can be deployed on word nodes, how to access them when edge node register(dynamic IP address)
	cmd := node.Command("bash", "-c", "kubectl taint nodes  node-role.kubernetes.io/master- --selector='node-role.kubernetes.io/control-plane='")
	lines, err := exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to taint master nodes: %v", err)
	}

	// use keadm init to install cloudcore in container mode
	// cloudcore svc use NodePort type, to enable edgecore connect to cloudcore, we may add the below routes on the host
	//iptables -t nat -A PREROUTING -d ${advertise-address} -p tcp --dport 10000 -j DNAT --to-destination ${NODE_IP}:30000
	//iptables -t nat -A PREROUTING -d ${advertise-address} -p tcp --dport 10002 -j DNAT --to-destination ${NODE_IP}:30002
	startCmd := fmt.Sprintf("keadm init --advertise-address=%s --profile version=v1.11.1 --kube-config /etc/kubernetes/admin.conf --set cloudCore.hostNetWork=false", a.AdvertiseAddress)
	cmd = node.Command("bash", "-c", startCmd)
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to keadm init: %v", err)
	}

	time.Sleep(20 * time.Second)
	cmd = node.Command("bash", "-c", `kubectl get secret -nkubeedge tokensecret -o=jsonpath='{.data.tokendata}' | base64 -d`)
	token, err := exec.Output(cmd)
	if err != nil {
		return fmt.Errorf("failed to get tokensecret: %v", err)
	}

	KubeEdgeToken = string(token)
	return nil
}

func (a *Action) BootstrapCloudCore(ctx *actions.ActionContext) error {
	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	// get the target node for this task
	// TODO: eliminate the concept of bootstrapcontrolplane node entirely
	// outside this method
	node, err := nodeutils.BootstrapControlPlaneNode(allNodes)
	if err != nil {
		return err
	}

	// create ns kubeedge
	cmd := node.Command("kubectl", "create", "ns", "kubeedge")
	lines, err := exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to create ns kubeedge: %v", err)
	}

	// create CRDs
	crds := []string{
		"devices_v1alpha2_device.yaml",
		"devices_v1alpha2_devicemodel.yaml",
		"cluster_objectsync_v1alpha1.yaml",
		"objectsync_v1alpha1.yaml",
		"router_v1_rule.yaml",
		"router_v1_ruleEndpoint.yaml",
	}

	// CRDs are copied to image when build image
	for _, crd := range crds {
		crdPath := filepath.Join("/etc/kubeedge/crds/", crd)
		cmd = node.Command("kubectl", "create", "-f", crdPath)
		lines, err := exec.CombinedOutputLines(cmd)
		ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
		if err != nil {
			return fmt.Errorf("failed to create CRD %s: %v", crd, err)
		}
	}

	// start cloudcore
	if err := startCloudcore(ctx, node); err != nil {
		return err
	}

	time.Sleep(20 * time.Second)
	cmd = node.Command("bash", "-c", `kubectl get secret -nkubeedge tokensecret -o=jsonpath='{.data.tokendata}' | base64 -d`)
	token, err := exec.Output(cmd)
	if err != nil {
		return fmt.Errorf("failed to get tokensecret: %v", err)
	}

	KubeEdgeToken = string(token)
	return nil
}

// startCloudcore on control plane
func startCloudcore(ctx *actions.ActionContext, node nodes.Node) error {
	// generate config
	cmd := node.Command("bash", "-c", "cloudcore --defaultconfig >  /etc/kubeedge/config/cloudcore.yaml")
	lines, err := exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to generate cloudcore config: %v", err)
	}

	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|kubeConfig: .*|kubeConfig: %s|g" /etc/kubeedge/config/cloudcore.yaml`, "/etc/kubernetes/admin.conf"))
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify kubeconfig: %v", err)
	}
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i '/iptablesManager:/{n;s/true/false/;}' /etc/kubeedge/config/cloudcore.yaml`))
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify kubeconfig: %v", err)
	}

	cmd = node.Command("bash", "-c", "systemctl daemon-reload && systemctl enable cloudcore && systemctl start cloudcore")
	lines, err = exec.CombinedOutputLines(cmd)
	ctx.Logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to start cloudcore: %v", err)
	}
	return nil
}

// BootstrapEdgecoreWithKeadm
func (a *Action) BootstrapEdgecoreWithKeadm(ctx *actions.ActionContext) error {
	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	// master node/ control plane
	controlPlane, err := nodeutils.ControlPlaneNodes(allNodes)
	if err != nil {
		return err
	}

	for _, v := range controlPlane {
		ip, _, _ := v.IP()
		controlPlaneIP = ip
	}
	if a.AdvertiseAddress != "" {
		controlPlaneIP = a.AdvertiseAddress
	}

	fmt.Printf("edge node will use IP %s to register to cloud\n", controlPlaneIP)
	// then join worker nodes if any
	// The below operation, we should exec in the worker nodes, but not master
	edgeNodes, err := nodeutils.SelectNodesByRole(allNodes, constants.EdgeNodeRoleValue)
	if err != nil {
		return err
	}
	if len(edgeNodes) > 0 {
		if err := a.joinEdgeNodes(ctx, edgeNodes); err != nil {
			return err
		}
	}

	return nil
}

// BootstrapEdgecore
func (a *Action) BootstrapEdgecore(ctx *actions.ActionContext) error {
	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	// master node/ control plane
	controlPlane, err := nodeutils.ControlPlaneNodes(allNodes)
	if err != nil {
		return err
	}

	for _, v := range controlPlane {
		ip, _, _ := v.IP()
		controlPlaneIP = ip
	}
	if a.AdvertiseAddress != "" {
		controlPlaneIP = a.AdvertiseAddress
	}

	fmt.Printf("edge node will use IP %s to register to cloud\n", controlPlaneIP)

	// then join edge nodes if any
	// The below operation, we should exec in the edge nodes, but not master
	edgeNodes, err := nodeutils.SelectNodesByRole(allNodes, constants.EdgeNodeRoleValue)
	if err != nil {
		return err
	}
	if len(edgeNodes) > 0 {
		if err := a.joinEdgeNodes(ctx, edgeNodes); err != nil {
			return err
		}
	}

	return nil
}

func (a *Action) joinEdgeNodes(
	ctx *actions.ActionContext,
	edgeNodes []nodes.Node,
) error {
	ctx.Status.Start("Joining edge nodes 🚜")
	defer ctx.Status.End(false)

	// create the workers concurrently
	fns := []func() error{}
	for _, node := range edgeNodes {
		node := node // capture loop variable
		fns = append(fns, func() error {
			if a.ContainerMode {
				return runStartEdgecoreWithKeadm(ctx.Logger, node)
			}
			return runStartEdgecore(ctx.Logger, node)
		})
	}
	if err := errors.UntilErrorConcurrent(fns); err != nil {
		return err
	}

	ctx.Status.End(true)
	return nil
}

// runKubeadmJoin executes kubeadm join command
func runStartEdgecore(logger log.Logger, node nodes.Node) error {
	// first stop kubelet
	cmd := node.Command("bash", "-c", "systemctl stop kubelet.service")
	lines, err := exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return errors.Wrap(err, "failed to join node with kubeadm")
	}

	// generate config
	cmd = node.Command("bash", "-c", "edgecore --defaultconfig > /etc/kubeedge/config/edgecore.yaml")
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to generate cloudcore config: %v", err)
	}

	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|token: .*|token: %s|g" /etc/kubeedge/config/edgecore.yaml`, KubeEdgeToken))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify token: %v", err)
	}

	// modify runtime to containerd
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|remoteImageEndpoint: .*|remoteImageEndpoint: %s|g" /etc/kubeedge/config/edgecore.yaml`, "unix:///var/run/containerd/containerd.sock"))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify remoteImageEndpoint: %v", err)
	}
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|remoteRuntimeEndpoint: .*|remoteRuntimeEndpoint: %s|g" /etc/kubeedge/config/edgecore.yaml`, "unix:///var/run/containerd/containerd.sock"))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify remoteRuntimeEndpoint: %v", err)
	}
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|runtimeType: .*|runtimeType: %s|g" /etc/kubeedge/config/edgecore.yaml`, "remote"))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify remoteImageEndpoint: %v", err)
	}

	// modify edgeHub.httpServer websocker.server ip cloudcore ip or control-plane ip
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|httpServer: .*|httpServer: %s|g" /etc/kubeedge/config/edgecore.yaml`, "https://"+controlPlaneIP+":10002"))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify httpServer: %v", err)
	}
	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|server: .*10000|server: %s|g" /etc/kubeedge/config/edgecore.yaml`, controlPlaneIP+":10000"))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify httpServer: %v", err)
	}

	cmd = node.Command("bash", "-c", fmt.Sprintf(`sed -i -e "s|mqttMode: .*|mqttMode: 0|g" /etc/kubeedge/config/edgecore.yaml`))
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to modify mqttMode: %v", err)
	}

	cmd = node.Command("bash", "-c", "systemctl daemon-reload && systemctl enable edgecore && systemctl start edgecore")
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to start cloudcore: %v", err)
	}

	return nil
}

// runStartEdgecoreWithKeadm executes kubeadm join command
func runStartEdgecoreWithKeadm(logger log.Logger, node nodes.Node) error {
	// first stop kubelet
	cmd := node.Command("bash", "-c", "systemctl stop kubelet.service")
	lines, err := exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return errors.Wrap(err, "failed to join node with kubeadm")
	}

	// rm /etc/kubeedge directory, or keadm join will report error
	cmd = node.Command("bash", "-c", "rm -rf /etc/kubeedge")
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to cleanup directory /etc/kubeedge: %v", err)
	}

	// not start MQTT conainer, error: E0728 01:07:37.717267    1429 remote_runtime.go:116] "RunPodSandbox from runtime service failed" err="rpc error: code = Unknown
	// desc = failed to reserve sandbox name \"mqtt___0\": name \"mqtt___0\" is reserved for \"264c9ad4f0be7271711a21b0c89f958da582e1869a3b18fb07dd719b16989595\""
	joinCmd := fmt.Sprintf("keadm join --cloudcore-ipport %s --certport 30002 --token %s --remote-runtime-endpoint unix:///var/run/containerd/containerd.sock --runtimetype remote --with-mqtt=false", controlPlaneIP+":30000", KubeEdgeToken)
	cmd = node.Command("bash", "-c", joinCmd)
	lines, err = exec.CombinedOutputLines(cmd)
	logger.V(3).Info(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to join edge: %v", err)
	}

	return nil
}
