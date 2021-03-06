package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestK8sClusterCreatesCorrectly(t *testing.T) {
	c, _, cleanup := setupTestConfig(t, clusterDefault)
	defer cleanup()

	cl, err := c.FindResource("k8s_cluster.testing")
	assert.NoError(t, err)

	assert.Equal(t, "testing", cl.Info().Name)
	assert.Equal(t, TypeK8sCluster, cl.Info().Type)
	assert.Equal(t, PendingCreation, cl.Info().Status)
}

const clusterDefault = `
k8s_cluster "testing" {
	network {
		name = "network.test"
	}
	driver = "k3s"
}
`
