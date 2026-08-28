package ollama

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/vsolanki12/codeatlas-assistant/internal/metrics"
)

type LLM interface {
	Generate(prompt string) error
	GenerateString(prompt string) (string, error)
}

type Client struct {
	Model     string
	NumCtx    int
	MaxOutput int

	usageMu sync.RWMutex
	usage   Usage
}

// Usage records the last Ollama request. Ollama supplies actual token counts
// on its terminal streaming chunk; estimates remain available when a mock or
// older server omits those fields.
type Usage struct {
	Prompt       metrics.Text `json:"prompt"`
	Output       metrics.Text `json:"output"`
	PromptTokens int          `json:"promptTokens,omitempty"`
	OutputTokens int          `json:"outputTokens,omitempty"`
}

// LastUsage returns measurements for the most recent generation request.
func (c *Client) LastUsage() Usage {
	c.usageMu.RLock()
	defer c.usageMu.RUnlock()
	return c.usage
}

type Options struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumCtx      int     `json:"num_ctx,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type request struct {
	Model   string  `json:"model"`
	Prompt  string  `json:"prompt"`
	Stream  bool    `json:"stream"`
	Options Options `json:"options"`
}

type response struct {
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func baseURL() string {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/")
}

func (c *Client) options() Options {
	return Options{
		Temperature: 0.1,
		NumCtx:      c.NumCtx,
		NumPredict:  c.MaxOutput,
	}
}

func ListModels() ([]string, error) {
	resp, err := http.Get(baseURL() + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("cannot connect to ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tags tagsResponse
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, err
	}

	models := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = m.Name
	}
	return models, nil
}

func (c *Client) Generate(prompt string) error {
	req := request{
		Model:   c.Model,
		Prompt:  prompt,
		Stream:  true,
		Options: c.options(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "prompt size: %s\n", metrics.FormatText(metrics.Measure(prompt)))
	c.setUsage(prompt, "", 0, 0)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Post(
		baseURL()+"/api/generate",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return fmt.Errorf("cannot connect to ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	var output strings.Builder
	var promptTokens, outputTokens int
	for scanner.Scan() {
		var chunk response
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		fmt.Fprint(os.Stdout, chunk.Response)
		output.WriteString(chunk.Response)
		if chunk.Done {
			promptTokens = chunk.PromptEvalCount
			outputTokens = chunk.EvalCount
			fmt.Println()
			break
		}
	}

	c.setUsage(prompt, output.String(), promptTokens, outputTokens)
	c.printUsage()
	return scanner.Err()
}

func (c *Client) GenerateString(prompt string) (string, error) {
	req := request{
		Model:   c.Model,
		Prompt:  prompt,
		Stream:  true,
		Options: c.options(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "prompt size: %s\n", metrics.FormatText(metrics.Measure(prompt)))
	c.setUsage(prompt, "", 0, 0)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Post(
		baseURL()+"/api/generate",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return "", fmt.Errorf("cannot connect to ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	var promptTokens, outputTokens int
	for scanner.Scan() {
		var chunk response
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		result.WriteString(chunk.Response)
		if chunk.Done {
			promptTokens = chunk.PromptEvalCount
			outputTokens = chunk.EvalCount
			break
		}
	}

	if err := scanner.Err(); err != nil {
		c.setUsage(prompt, result.String(), promptTokens, outputTokens)
		c.printUsage()
		return "", err
	}
	c.setUsage(prompt, result.String(), promptTokens, outputTokens)
	c.printUsage()
	return result.String(), nil
}

func (c *Client) setUsage(prompt, output string, promptTokens, outputTokens int) {
	c.usageMu.Lock()
	c.usage = Usage{
		Prompt:       metrics.Measure(prompt),
		Output:       metrics.Measure(output),
		PromptTokens: promptTokens,
		OutputTokens: outputTokens,
	}
	c.usageMu.Unlock()
}

func (c *Client) printUsage() {
	usage := c.LastUsage()
	promptTokens := usage.Prompt.EstimatedTokens
	outputTokens := usage.Output.EstimatedTokens
	if usage.PromptTokens > 0 {
		promptTokens = usage.PromptTokens
	}
	if usage.OutputTokens > 0 {
		outputTokens = usage.OutputTokens
	}
	fmt.Fprintf(os.Stderr, "LLM usage: prompt=%d tokens, output=%d tokens\n", promptTokens, outputTokens)
}
