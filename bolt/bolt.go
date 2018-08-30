package bolt

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Result struct {
	Items []struct {
		Node   string
		Status string
		Result map[string]string
	}
	Node_count   int
	Elapsed_time int
}

func runCommand(command string) ([]byte, error) {
	var cmdargs []string

	if runtime.GOOS == "windows" {
		cmdargs = []string{"cmd", "/C"}
	} else {
		cmdargs = []string{"/bin/sh", "-c"}
	}
	cmdargs = append(cmdargs, command)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdargs[0], cmdargs[1:]...)
	return cmd.Output()
}

func commandExists(command string) bool {
	var cmdargs []string

	if runtime.GOOS == "windows" {
		cmdargs = []string{"where", "/q"}
	} else {
		cmdargs = []string{"command", "-v"}
	}
	cmdargs = append(cmdargs, command)

	_, err := runCommand(strings.Join(cmdargs, " "))
	return err == nil
}

func Task(node string, user string, sudo bool, task string, args map[string]string) (*Result, error) {
	if !commandExists("bolt") {
		return nil, fmt.Errorf("bolt command not available in PATH")
	}

	cmdargs := []string{"bolt", "task", "run", "--nodes", node, "-u", user}

	if sudo {
		cmdargs = append(cmdargs, "--run-as", "root")
	}

	cmdargs = append(cmdargs, "--no-host-key-check", "--format", "json", task)

	if args != nil {
		for key, value := range args {
			cmdargs = append(cmdargs, strings.Join([]string{key, value}, "="))
		}
	}

	out, err := runCommand(strings.Join(cmdargs, " "))
	if err != nil {
		return nil, fmt.Errorf("Bolt: \"%s\": %s: %s", strings.Join(cmdargs, " "), out, err)
	}

	result := new(Result)
	if err = json.Unmarshal(out, result); err != nil {
		return nil, err
	}

	return result, nil
}
