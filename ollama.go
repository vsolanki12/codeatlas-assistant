package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type OllamaTagsResponse struct {
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

	var tags OllamaTagsResponse
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, err
	}

	models := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = m.Name
	}
	return models, nil
}

func AskOllamaStream(model string, prompt string) error {
	req := OllamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := http.Post(
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
		var chunk OllamaResponse
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
