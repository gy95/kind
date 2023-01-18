package docker

import (
	"fmt"
	"strings"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
	"sigs.k8s.io/kind/pkg/shared/apis/config"
)

// this package contains some functions with uppercase, so that outer packages can import these functions directly

func PlanCreation(cfg *config.Cluster, networkName string) (createContainerFuncs []func() error, err error) {
	return planCreation(cfg, networkName)
}

func (p *provider) Node(name string) nodes.Node {
	return p.node(name)
}

// label key/value to indicate a KubeEdge edge node
const EdgeNodeLabelKey = "node.kubernetes.io/edge"
const EdgeNodeLabelValue = ""

func ListEdgeNodesByLabel(cluster string) ([]nodes.Node, error) {
	cmd := exec.Command("kubectl",
		"get",
		"node",
		// filter for nodes with the edge-node label
		"-l", fmt.Sprintf("%s=%s", EdgeNodeLabelKey, EdgeNodeLabelValue),
		// only output the resource Node name, like node/kind-worker with prefix node/
		"-o=name",
	)
	lines, err := exec.OutputLines(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list nodes")
	}

	// convert names to node handles
	ret := make([]nodes.Node, 0, len(lines))
	for _, name := range lines {
		nodeName := strings.TrimPrefix(name, "node/")
		ret = append(ret, &node{
			name: nodeName,
		})
	}
	return ret, nil
}
