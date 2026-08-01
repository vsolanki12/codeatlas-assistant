package main

import (
	"bytes"
	"fmt"
	"os/exec"
)

func runAtlas(graphPath string, args ...string) (string, error) {
	fullArgs := append(args[:1], append([]string{"--graph", graphPath}, args[1:]...)...)

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

func RunForIntent(graphPath string, entity string, intent Intent) (string, error) {
	switch intent {
	case IntentExplain:
		return runAtlas(graphPath, "explain", entity)
	case IntentImpact:
		return runAtlas(graphPath, "impact", entity)
	case IntentInvestigate:
		return runAtlas(graphPath, "investigate", entity)
	case IntentSearch:
		return runAtlas(graphPath, "search", entity)
	case IntentStats:
		return runAtlas(graphPath, "stats")
	default:
		return runAtlas(graphPath, "ask", entity)
	}
}
