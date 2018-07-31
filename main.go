package main

import (
	"github.com/hashicorp/terraform/terraform"
	"github.com/hashicorp/terraform/plugin"
)

func ResourceProvisionerBuilder() terraform.ResourceProvisioner {
	return Provisioner()
}

func main() {
	serveOpts := &plugin.ServeOpts{
		ProvisionerFunc: ResourceProvisionerBuilder,
	}

	plugin.Serve(serveOpts)
}
