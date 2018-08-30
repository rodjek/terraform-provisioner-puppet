package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/hashicorp/terraform/communicator"
	"github.com/hashicorp/terraform/communicator/remote"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
	"github.com/mitchellh/go-linereader"
	"github.com/rodjek/terraform-provisioner-puppet/bolt"
	"gopkg.in/yaml.v2"
)

type provisionFn func(terraform.UIOutput, communicator.Communicator) error

type provisioner struct {
	UseSudo    bool
	Server     string
	OSType     string
	Autosign   bool
	OpenSource bool

	instanceState *terraform.InstanceState

	runPuppetAgent     provisionFn
	installPuppetAgent provisionFn
}

type csrAttributes struct {
	CustomAttributes map[string]string `yaml:"custom_attributes"`
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
			"autosign": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
			},
			"open_source": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
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

	config.instanceState = state

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

	if config.OpenSource {
		config.installPuppetAgent = config.installPuppetAgentOpenSource
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

	csrAttrs := new(csrAttributes)
	csrAttrs.CustomAttributes = make(map[string]string)

	if config.Autosign {
		autosignToken, err := config.generateAutosignToken(state.Attributes["private_dns"], state.Ephemeral.ConnInfo["user"])
		if err != nil {
			return fmt.Errorf("Failed to generate an autosign token: %s %s", err)
		}
		csrAttrs.CustomAttributes["challengePassword"] = autosignToken
	}

	err = config.writeCSRAttributes(csrAttrs, comm, output)
	if err != nil {
		return fmt.Errorf("Failed to write csr_attributes.yaml: %s", err)
	}

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

func (p *provisioner) writeCSRAttributes(attrs *csrAttributes, comm communicator.Communicator, output terraform.UIOutput) error {
	file, err := ioutil.TempFile("", "puppet-crt-attrs")
	if err != nil {
		return fmt.Errorf("Failed to create a temp file: %s", err)
	}
	defer os.Remove(file.Name())

	content, err := yaml.Marshal(attrs)
	if err != nil {
		return fmt.Errorf("Failed to marshal CSR attributes to YAML: %s", err)
	}

	_, err = file.WriteString(string(content))
	if err != nil {
		return fmt.Errorf("Failed to write YAML to temp file: %s", err)
	}

	file.Seek(0, 0)
	err = comm.Upload("/tmp/csr_attributes.yaml", file)
	if err != nil {
		return err
	}

	if err = p.runCommand(output, comm, "mkdir -p /etc/puppetlabs/puppet"); err != nil {
		return err
	}

	return p.runCommand(output, comm, "mv /tmp/csr_attributes.yaml /etc/puppetlabs/puppet/")
}

func (p *provisioner) generateAutosignToken(certname string, user string) (string, error) {
	result, err := bolt.Task("ssh://"+p.Server, user, p.UseSudo, "autosign::generate_token", map[string]string{"certname": certname})
	if err != nil {
		return "", err
	}
	// TODO check error state in JSON
	return result.Items[0].Result["_output"], nil
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

func (p *provisioner) installPuppetAgentOpenSource(output terraform.UIOutput, comm communicator.Communicator) error {
	result, err := bolt.Task(
		"ssh://"+p.instanceState.Ephemeral.ConnInfo["host"],
		p.instanceState.Ephemeral.ConnInfo["user"],
		p.UseSudo,
		"puppet_agent::install",
		nil,
	)

	if err != nil {
		return fmt.Errorf("puppet_agent::install failed: %s\n%+v", err, result)
	}

	return nil
}

func (p *provisioner) nixRunPuppetAgent(output terraform.UIOutput, comm communicator.Communicator) error {
	err := p.runCommand(output, comm, "/opt/puppetlabs/puppet/bin/puppet agent -t --server "+p.Server)

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
		UseSudo:    d.Get("use_sudo").(bool),
		Server:     d.Get("server").(string),
		OSType:     d.Get("os_type").(string),
		Autosign:   d.Get("autosign").(bool),
		OpenSource: d.Get("open_source").(bool),
	}

	return p, nil
}
