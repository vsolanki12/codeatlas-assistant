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

type Client struct {
	Path string
}

func (c *Client) Run(args ...string) (string, error) {
	fullArgs := append(args[:1], append([]string{"--graph", c.Path}, args[1:]...)...)

	cmd := exec.Command("atlas", fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s", stderr.String())
	}

	return stdout.String(), nil
}

func (c *Client) GraphPath() string {
	return c.Path
}
