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
	"time"
)

type LLM interface {
	Generate(prompt string) error
	GenerateString(prompt string) (string, error)
}

type Client struct {
	Model string
}

type Options struct {
	Temperature float64 `json:"temperature"`
}

type request struct {
	Model   string  `json:"model"`
	Prompt  string  `json:"prompt"`
	Stream  bool    `json:"stream"`
	Options Options `json:"options"`
}

type response struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func ListModels() ([]string, error) {
	resp, err := http.Get("http://localhost:11434/api/tags")
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
		Model:  c.Model,
		Prompt: prompt,
		Stream: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "prompt size: %d chars\n", len(prompt))

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Post(
		"http://localhost:11434/api/generate",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return fmt.Errorf("cannot connect to ollama (is it running?): %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var chunk response
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		fmt.Fprint(os.Stdout, chunk.Response)
		if chunk.Done {
			fmt.Println()
			break
		}
	}

	return scanner.Err()
}

func (c *Client) GenerateString(prompt string) (string, error) {
	req := request{
		Model:  c.Model,
		Prompt: prompt,
		Stream: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "prompt size: %d chars\n", len(prompt))

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Post(
		"http://localhost:11434/api/generate",
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
	for scanner.Scan() {
		var chunk response
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		result.WriteString(chunk.Response)
		if chunk.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	return result.String(), nil
}
