package providers

import (
	"fmt"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/shipyard-run/shipyard/pkg/clients"
	"github.com/shipyard-run/shipyard/pkg/config"
	"github.com/shipyard-run/shipyard/pkg/utils"
	"golang.org/x/xerrors"
)

const ingressImage = "registry.shipyard.run/ingress:latest"

// Ingress defines a provider for handling connection ingress for a cluster
type Ingress struct {
	config *config.Ingress
	client clients.ContainerTasks
	log    hclog.Logger
}

// NewIngress creates a new ingress provider
func NewIngress(c *config.Ingress, cc clients.ContainerTasks, l hclog.Logger) *Ingress {
	return &Ingress{c, cc, l}
}

// NewContainerIngress creates a new ingress provider for a container
func NewContainerIngress(ci *config.ContainerIngress, cc clients.ContainerTasks, l hclog.Logger) *Ingress {
	c := config.NewIngress(ci.Name)

	c.Depends = ci.Depends
	c.Networks = ci.Networks
	c.Target = ci.Target
	c.Ports = ci.Ports
	c.Config = ci.Config

	return &Ingress{c, cc, l}
}

// NewNomadIngress creates an ingress type for resources in a Nomad cluster
func NewNomadIngress(ci *config.NomadIngress, cc clients.ContainerTasks, l hclog.Logger) *Ingress {
	c := config.NewIngress(ci.Name)
	c.Depends = ci.Depends
	c.Networks = ci.Networks
	c.Target = ci.Cluster
	c.Ports = ci.Ports
	c.Config = ci.Config

	c.Service = fmt.Sprintf("%s.%s.%s", ci.Job, ci.Group, ci.Task)

	return &Ingress{c, cc, l}
}

// NewK8sIngress creates an Ingress from Kubernetes config
func NewK8sIngress(kc *config.K8sIngress, cc clients.ContainerTasks, l hclog.Logger) *Ingress {
	// convert the config
	c := config.NewIngress(kc.Name)

	c.Depends = kc.Depends
	c.Networks = kc.Networks
	c.Target = kc.Cluster

	if kc.Deployment != "" {
		c.Service = fmt.Sprintf("deployment/%s", kc.Deployment)
	}

	if kc.Service != "" {
		c.Service = fmt.Sprintf("svc/%s", kc.Service)
	}

	if kc.Pod != "" {
		c.Service = kc.Pod
	}

	c.Namespace = kc.Namespace
	c.Ports = kc.Ports

	c.Config = kc.Config

	return &Ingress{c, cc, l}
}

// Create the ingress
func (i *Ingress) Create() error {
	i.log.Info("Creating Ingress", "ref", i.config.Name)

	// check the ingress does not already exist
	// TODO, we can probably extract all of the check and pull logic into a common function
	ids, err := i.client.FindContainerIDs(i.config.Name, i.config.Type)
	if len(ids) > 0 {
		return xerrors.Errorf("Unable to create ingress, and ingress with the name %s already exists: %w", i.config.Name, err)
	}

	if err != nil {
		return xerrors.Errorf("Unable to lookup ingress id: %w", err)
	}

	// pull any images needed for this container
	err = i.client.PullImage(config.Image{Name: ingressImage}, false)
	if err != nil {
		i.log.Error("Error pulling container image", "ref", i.config.Name, "image", ingressImage)

		return err
	}

	var serviceName string
	var volumes []config.Volume
	var env []config.KV
	command := make([]string, 0)

	target, err := i.config.FindDependentResource(i.config.Target)
	if err != nil {
		return err
	}

	switch target.Info().Type {
	case config.TypeContainer:
		serviceName = utils.FQDN(target.Info().Name, string(target.Info().Type))
	case config.TypeNomadCluster:
		v := target.(*config.NomadCluster)
		// if this is a nomad cluster we need to add the nomadconfig and
		// make sure that the proxy runs in nomad mode
		serviceName = i.config.Service
		_, nomadConfigPath := utils.CreateClusterConfigPath(v.Name)
		nomadConfigDestPath := "/.nomad/config.json"

		volumes = append(volumes, config.Volume{
			Source:      nomadConfigPath,
			Destination: nomadConfigDestPath,
		})

		command = append(command, "--proxy-type")
		command = append(command, "nomad")

		command = append(command, "--nomad-config")
		command = append(command, "/.nomad/config.json")

	case config.TypeK8sCluster:
		v := target.(*config.K8sCluster)
		// if this is a k3s cluster we need to add the kubeconfig and
		// make sure that the proxy runs in kube mode
		serviceName = i.config.Service
		_, _, kubeConfigPath := utils.CreateKubeConfigPath(v.Name)
		volumes = append(volumes, config.Volume{
			Source:      kubeConfigPath,
			Destination: "/.kube/kubeconfig.yml",
		})

		env = append(env, config.KV{Key: "KUBECONFIG", Value: "/.kube/kubeconfig.yml"})

		command = append(command, "--proxy-type")
		command = append(command, "kubernetes")

		// if the namespace is not present assume default
		if i.config.Namespace == "" {
			i.config.Namespace = "default"
		}

		command = append(command, "--namespace")
		command = append(command, i.config.Namespace)

	default:
		return fmt.Errorf("Only Containers, Kubernetes clusters, and Nomad clusters are supported at present")
	}

	command = append(command, "--service-name")
	command = append(command, serviceName)

	// add the ports
	for _, p := range i.config.Ports {
		command = append(command, "--ports")
		command = append(command, fmt.Sprintf("%s:%s", p.Local, p.Remote))
	}

	// ingress simply crease a container with specific options
	c := config.NewContainer(i.config.Name)
	i.config.ResourceInfo.AddChild(c)

	c.Networks = i.config.Networks
	c.Ports = i.config.Ports
	c.Image = &config.Image{Name: ingressImage}
	c.Command = command
	c.Volumes = volumes
	c.Environment = env

	_, err = i.client.CreateContainer(c)
	if err != nil {
		return err
	}

	// set the state
	i.config.Status = config.Applied

	return nil
}

// Destroy the ingress
func (i *Ingress) Destroy() error {
	i.log.Info("Destroy Ingress", "ref", i.config.Name, "type", i.config.Type)

	ids, err := i.client.FindContainerIDs(i.config.Name, i.config.Type)
	if err != nil {
		return err
	}

	for _, id := range ids {
		for _, n := range i.config.Networks {
			i.log.Debug("Detaching container from network", "ref", i.config.Name, "id", id, "network", n.Name)
			err := i.client.DetachNetwork(n.Name, id)
			if err != nil {
				i.log.Error("Unable to detach network", "ref", i.config.Name, "network", n.Name, "error", err)
			}
		}

		err := i.client.RemoveContainer(id)
		if err != nil {
			return err
		}

	}

	return nil
}

// Lookup the id of the ingress
func (i *Ingress) Lookup() ([]string, error) {
	return []string{}, nil
}

// Config returns the config for the provider
func (i *Ingress) Config() ConfigWrapper {
	return ConfigWrapper{"config.Ingress", i.config}
}
