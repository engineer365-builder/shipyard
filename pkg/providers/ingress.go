package providers

import (
	"fmt"

	"github.com/shipyard-run/cli/pkg/clients"
	"github.com/shipyard-run/cli/pkg/config"
)

type Ingress struct {
	config *config.Ingress
	client clients.Docker
}

func NewIngress(c *config.Ingress, cc clients.Docker) *Ingress {
	return &Ingress{c, cc}
}

func (i *Ingress) Create() error {

	var serviceName string
	var volumes []config.Volume
	var env []config.KV
	command := make([]string, 0)

	switch v := i.config.TargetRef.(type) {
	case *config.Container:
		serviceName = FQDN(v.Name, v.NetworkRef.Name)
	case *config.Cluster:
		serviceName = i.config.Service

		_, _, kubeConfigPath := CreateKubeConfigPath(v.Name)
		volumes = append(volumes, config.Volume{
			Source:      kubeConfigPath,
			Destination: "/.kube/kubeconfig.yml",
		})

		env = append(env, config.KV{Key: "KUBECONFIG", Value: "/.kube/kubeconfig.yml"})

		command = append(command, "--proxy-type")
		command = append(command, "kubernetes")
	default:
		return fmt.Errorf("Only Container ingress and K3s are supported at present")
	}

	image := "shipyardrun/ingress:latest"

	command = append(command, "--service-name")
	command = append(command, serviceName)

	// add the ports
	for _, p := range i.config.Ports {
		command = append(command, "--ports")
		command = append(command, fmt.Sprintf("%d:%d", p.Local, p.Remote))
	}

	// ingress simply crease a container with specific options
	c := &config.Container{
		Name:        i.config.Name,
		NetworkRef:  i.config.NetworkRef,
		Ports:       i.config.Ports,
		Image:       image,
		Command:     command,
		Volumes:     volumes,
		Environment: env,
	}

	p := NewContainer(c, i.client)

	return p.Create()
}

func (i *Ingress) Destroy() error {
	c := &config.Container{
		Name:       i.config.Name,
		NetworkRef: i.config.NetworkRef,
	}

	p := NewContainer(c, i.client)

	return p.Destroy()
}

func (i *Ingress) Lookup() (string, error) {
	return "", nil
}