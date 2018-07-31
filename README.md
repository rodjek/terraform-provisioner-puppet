# terraform-provisioner-puppet

Very basic POC - Installs the agent, runs the agent. If the master is
configured to do domain based autosigning (as in the included example), then
the agent run will be successful.

## Installation

```
go get github.com/rodjek/terraform-provisioner-puppet
```

## Example

This will spin up 2 instances on EC2 - 1 is a Puppet Enterprise master
(configured manually using the `remote-exec` provisioner) and the other is an
agent configured automatically using the `puppet` provisioner.

```
cd $GOPATH/src/github.com/rodjek/terraform-provisioner-puppet/example
terraform init
```

Create a `terraform.tfvars` file with the following values
```
access_key = "<AWS Access Key ID>"
secret_key = "<AWS Secret Access Key>"
region = "<AWS Region>"
aws_key_pair = "<AWS keypair name>"
aws_ami_id = "<AWS AMI ID>"
```

Let terraform do its thing
```
terraform apply
```
