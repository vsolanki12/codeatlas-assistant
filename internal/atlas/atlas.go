package atlas

import (
	"bytes"
	"fmt"
	"os/exec"
)

type Runner interface {
	Run(args ...string) (string, error)
	GraphPath() string
}

// JSONRunner is implemented by Atlas clients that can return the typed query
// result directly. Keeping it optional preserves the small Runner interface
// used by existing mocks and integrations.
type JSONRunner interface {
	Runner
	RunJSON(args ...string) (string, error)
}

type Client struct {
	Path string
}

func (c *Client) Run(args ...string) (string, error) {
	return c.run(args, false)
}

func (c *Client) RunJSON(args ...string) (string, error) {
	return c.run(args, true)
}

func (c *Client) run(args []string, jsonOutput bool) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("atlas command is required")
	}
	fullArgs := make([]string, 0, len(args)+3)
	fullArgs = append(fullArgs, args[0], "--graph", c.Path)
	fullArgs = append(fullArgs, args[1:]...)
	if jsonOutput {
		fullArgs = append(fullArgs, "--json")
	}

	cmd := exec.Command("atlas", fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s", stderr.String())
		}
		return "", fmt.Errorf("atlas %s: %w", args[0], err)
	}

	return stdout.String(), nil
}

func (c *Client) GraphPath() string {
	return c.Path
}
