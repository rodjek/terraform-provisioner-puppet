package main

import (
	"context"
	"fmt"
	"io"

	"github.com/hashicorp/terraform/communicator"
	"github.com/hashicorp/terraform/communicator/remote"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/mitchellh/go-linereader"
)

type provisionFn func(terraform.UIOutput, communicator.Communicator) error

type provisioner struct {
	UseSudo bool
	Server  string
	OSType  string

	runPuppetAgent     provisionFn
	installPuppetAgent provisionFn
}

func Provisioner() terraform.ResourceProvisioner {
	return &schema.Provisioner{
		Schema: map[string]*schema.Schema{
			"server": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"os_type": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"use_sudo": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
		},
		ApplyFunc:    applyFn,
		ValidateFunc: validateFn,
	}
}

func applyFn(ctx context.Context) error {
	output := ctx.Value(schema.ProvOutputKey).(terraform.UIOutput)
	state := ctx.Value(schema.ProvRawStateKey).(*terraform.InstanceState)
	configData := ctx.Value(schema.ProvConfigDataKey).(*schema.ResourceData)

	config, err := decodeConfig(configData)
	if err != nil {
		return err
	}

	if config.OSType == "" {
		switch conn_type := state.Ephemeral.ConnInfo["type"]; conn_type {
		case "ssh", "":
			config.OSType = "nix"
		case "winrm":
			config.OSType = "windows"
		default:
			return fmt.Errorf("Unsupported connection type: %s", conn_type)
		}
	}

	switch config.OSType {
	case "nix":
		config.runPuppetAgent = config.nixRunPuppetAgent
		config.installPuppetAgent = config.nixInstallPuppetAgent
	case "windows":
		config.runPuppetAgent = config.windowsRunPuppetAgent
		config.installPuppetAgent = config.windowsInstallPuppetAgent
	default:
		return fmt.Errorf("Unsupported OS type: %s", config.OSType)
	}

	comm, err := communicator.New(state)
	if err != nil {
		return err
	}

	retryCtx, cancel := context.WithTimeout(ctx, comm.Timeout())
	defer cancel()

	err = communicator.Retry(retryCtx, func() error {
		return comm.Connect(output)
	})
	if err != nil {
		return err
	}
	defer comm.Disconnect()

	err = config.installPuppetAgent(output, comm)
	if err != nil {
		return err
	}

	err = config.runPuppetAgent(output, comm)
	if err != nil {
		return err
	}

	return nil
}

func validateFn(config *terraform.ResourceConfig) (ws []string, es []error) {
	return ws, es
}

func (p *provisioner) nixInstallPuppetAgent(output terraform.UIOutput, comm communicator.Communicator) error {
	err := p.runCommand(output, comm, fmt.Sprintf("curl -kO https://%s:8140/packages/current/install.bash", p.Server))
	if err != nil {
		return err
	}

	err = p.runCommand(output, comm, "bash -- ./install.bash --puppet-service-ensure stopped")
	if err != nil {
		return err
	}

	err = p.runCommand(output, comm, "rm -f install.bash")
    return err
}

func (p *provisioner) nixRunPuppetAgent(output terraform.UIOutput, comm communicator.Communicator) error {
    err := p.runCommand(output, comm, "puppet agent -t")

    if err != nil {
        errStruct, _ := err.(*remote.ExitError)
        if errStruct.ExitStatus == 2 {
            return nil
        }
        return err
    }

    return nil
}

func (p *provisioner) windowsInstallPuppetAgent(output terraform.UIOutput, comm communicator.Communicator) error {
    return nil
}

func (p *provisioner) windowsRunPuppetAgent(output terraform.UIOutput, comm communicator.Communicator) error {
    return nil
}

func (p *provisioner) runCommand(output terraform.UIOutput, comm communicator.Communicator, command string) error {
	if p.UseSudo {
		command = "sudo " + command
	}

	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	go p.copyOutput(output, outR)
	go p.copyOutput(output, errR)
	defer outW.Close()
	defer errW.Close()

	cmd := &remote.Cmd{
		Command: command,
		Stdout:  outW,
		Stderr:  errW,
	}

	err := comm.Start(cmd)
	if err != nil {
		return fmt.Errorf("Error executing command %q: %v", cmd.Command, err)
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}

func (p *provisioner) copyOutput(output terraform.UIOutput, reader io.Reader) {
	lr := linereader.New(reader)
	for line := range lr.Ch {
		output.Output(line)
	}
}

func decodeConfig(d *schema.ResourceData) (*provisioner, error) {
	p := &provisioner{
		UseSudo: d.Get("use_sudo").(bool),
		Server:  d.Get("server").(string),
		OSType:  d.Get("os_type").(string),
	}

    return p, nil
}
