package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

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

// Generate streams the model response to stdout as it arrives.
func (c *Client) Generate(prompt string) error {
	_, err := c.generate(context.Background(), prompt, os.Stdout)
	if err == nil {
		fmt.Println()
	}
	return err
}

// GenerateString collects the model response instead of streaming it.
func (c *Client) GenerateString(prompt string) (string, error) {
	return c.generate(context.Background(), prompt, nil)
}

// GenerateContext streams to echo, if non-nil, and honours cancellation.
func (c *Client) GenerateContext(ctx context.Context, prompt string, echo io.Writer) (string, error) {
	return c.generate(ctx, prompt, echo)
}

// generate performs one streaming request. echo receives each chunk as it
// arrives; a nil echo collects silently. The response is always returned so
// callers never have to choose between streaming and keeping the text.
func (c *Client) generate(ctx context.Context, prompt string, echo io.Writer) (string, error) {
	body, err := json.Marshal(request{
		Model:   c.Model,
		Prompt:  prompt,
		Stream:  true,
		Options: c.options(),
	})
	if err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "prompt size: %s\n", metrics.FormatText(metrics.Measure(prompt)))
	c.setUsage(prompt, "", 0, 0)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	// No client-side deadline: a long local generation is normal and the
	// caller's context is what decides when to stop.
	resp, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("cannot connect to ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	var output strings.Builder
	var promptTokens, outputTokens int
	// A streamed chunk is one JSON object per line and can exceed the default
	// scanner limit, which would silently truncate the response.
	decoder := json.NewDecoder(bufio.NewReaderSize(resp.Body, 64*1024))
	for {
		var chunk response
		if decodeErr := decoder.Decode(&chunk); decodeErr == io.EOF {
			break
		} else if decodeErr != nil {
			c.setUsage(prompt, output.String(), promptTokens, outputTokens)
			c.printUsage()
			return "", decodeErr
		}
		if echo != nil {
			fmt.Fprint(echo, chunk.Response)
		}
		output.WriteString(chunk.Response)
		if chunk.Done {
			promptTokens = chunk.PromptEvalCount
			outputTokens = chunk.EvalCount
			break
		}
	}

	c.setUsage(prompt, output.String(), promptTokens, outputTokens)
	c.printUsage()
	return output.String(), nil
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
	fmt.Fprintf(os.Stderr, "LLM token estimates: prompt=%d, output=%d\n", usage.Prompt.EstimatedTokens, usage.Output.EstimatedTokens)
	if usage.PromptTokens > 0 || usage.OutputTokens > 0 {
		fmt.Fprintf(os.Stderr, "Ollama reported token counts: prompt=%d, output=%d\n", usage.PromptTokens, usage.OutputTokens)
	}
}
