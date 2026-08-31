package atlas

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/vsolanki12/codeatlas-assistant/internal/metrics"
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
	// Path is the graph passed to every Atlas command.
	Path string
	// Binary optionally selects the Atlas executable. When empty, CODEATLAS_BIN
	// is used, then the "atlas" executable resolved through PATH.
	Binary string

	usageMu sync.RWMutex
	usage   Usage
}

// Usage records the deterministic Atlas calls made by one assistant process.
// It is intentionally a response-size measurement, not a claim about the
// model tokenizer or provider billing.
type Usage struct {
	Calls    int            `json:"calls"`
	Response metrics.Text   `json:"response"`
	Commands map[string]int `json:"commands,omitempty"`
}

// LastUsage returns cumulative Atlas measurements for this client.
func (c *Client) LastUsage() Usage {
	c.usageMu.RLock()
	defer c.usageMu.RUnlock()
	usage := c.usage
	usage.Commands = make(map[string]int, len(c.usage.Commands))
	for command, count := range c.usage.Commands {
		usage.Commands[command] = count
	}
	return usage
}

// Summary formats the measurements for a human-facing CLI diagnostic.
func (u Usage) Summary() string {
	commands := make([]string, 0, len(u.Commands))
	for command := range u.Commands {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		parts = append(parts, fmt.Sprintf("%s=%d", command, u.Commands[command]))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d calls, %s response", u.Calls, metrics.FormatText(u.Response))
	}
	return fmt.Sprintf("%d calls, %s response, commands: %s", u.Calls, metrics.FormatText(u.Response), strings.Join(parts, ", "))
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

	cmd := exec.Command(c.binaryPath(), fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	c.recordUsage(args[0], output)
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%s", stderr.String())
		}
		return "", fmt.Errorf("atlas %s: %w", args[0], err)
	}

	return output, nil
}

func (c *Client) binaryPath() string {
	if strings.TrimSpace(c.Binary) != "" {
		return c.Binary
	}
	if binary := strings.TrimSpace(os.Getenv("CODEATLAS_BIN")); binary != "" {
		return binary
	}
	return "atlas"
}

func (c *Client) recordUsage(command, output string) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if c.usage.Commands == nil {
		c.usage.Commands = make(map[string]int)
	}
	c.usage.Calls++
	c.usage.Commands[command]++
	response := metrics.Measure(output)
	c.usage.Response.Bytes += response.Bytes
	c.usage.Response.Characters += response.Characters
	c.usage.Response.EstimatedTokens += response.EstimatedTokens
}

func (c *Client) GraphPath() string {
	return c.Path
}
