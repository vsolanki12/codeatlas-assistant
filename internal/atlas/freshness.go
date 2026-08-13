package atlas

import (
	"fmt"
	"os/exec"
	"strings"
)

type Freshness struct {
	GraphCommit string
	RepoHead    string
	Stale       bool
}

func (f Freshness) Warning() string {
	if !f.Stale {
		return ""
	}
	return fmt.Sprintf("graph was built at %s but repo HEAD is %s — results may be outdated",
		short(f.GraphCommit), short(f.RepoHead))
}

func CheckFreshness(a Runner, repoPath string) Freshness {
	graphCommit := parseCommitFromStats(a)
	if graphCommit == "" {
		return Freshness{}
	}

	head := repoHead(repoPath)
	if head == "" {
		return Freshness{GraphCommit: graphCommit}
	}

	return Freshness{
		GraphCommit: graphCommit,
		RepoHead:    head,
		Stale:       graphCommit != head,
	}
}

func parseCommitFromStats(a Runner) string {
	out, err := a.Run("stats")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "commit: ") {
			return strings.TrimPrefix(line, "commit: ")
		}
	}
	return ""
}

func repoHead(repoPath string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func short(commit string) string {
	if len(commit) > 10 {
		return commit[:10]
	}
	return commit
}
