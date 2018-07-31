package main

import (
	"github.com/hashicorp/terraform/plugin"
	"github.com/hashicorp/terraform/terraform"
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
