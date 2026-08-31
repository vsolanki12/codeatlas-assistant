package atlas

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Freshness struct {
	GraphCommit        string
	RepoHead           string
	GraphRepository    string
	SchemaVersion      string
	SchemaCurrent      bool
	EntityIdentity     string
	ScanCoverage       *ScanCoverage
	ScanWarnings       []string
	Stale              bool
	Incomplete         bool
	RepositoryMismatch bool
	Dirty              bool
	StateVerifiable    bool
	Verifiable         bool
	Available          bool
}

// CurrentSchemaVersion is the graph contract supported by this Assistant.
// Keeping the version check at the consumer boundary prevents an older graph
// from silently omitting fields used by routing and validation.
const CurrentSchemaVersion = "1.5.0"

// ScanCoverage mirrors the small, stable coverage summary emitted by
// CodeAtlas. The assistant reports it as metadata; it never treats ignored
// files as parsed facts or turns counts into inferred repository behavior.
type ScanCoverage struct {
	Discovered int `json:"discovered"`
	Parsed     int `json:"parsed"`
	Reused     int `json:"reused"`
	Ignored    int `json:"ignored"`
	Failed     int `json:"failed"`
}

func (f Freshness) Warning() string {
	if !f.Available {
		return "CodeAtlas graph metadata is unavailable — repository facts cannot be grounded"
	}
	if f.Stale {
		return fmt.Sprintf("graph was built at %s but repo HEAD is %s — results may be outdated",
			short(f.GraphCommit), short(f.RepoHead))
	}
	if f.RepositoryMismatch {
		return fmt.Sprintf("graph was built from %s but the requested repository is different — facts may not apply",
			f.GraphRepository)
	}
	if f.Dirty {
		return "repository files differ from the CodeAtlas graph — results may be outdated"
	}
	if f.Incomplete {
		if f.ScanCoverage != nil && f.ScanCoverage.Failed > 0 {
			return fmt.Sprintf("graph scan is incomplete — %d discovered file(s) failed to parse; some repository facts may be absent", f.ScanCoverage.Failed)
		}
		return "graph scan is incomplete — some repository facts may be absent"
	}
	if f.SchemaVersion == "" {
		return "graph schema version is unavailable — rescan before relying on implementation guidance"
	}
	if !f.SchemaCurrent {
		return fmt.Sprintf("graph schema %s is not current — rescan with CodeAtlas schema %s", f.SchemaVersion, CurrentSchemaVersion)
	}
	if f.ScanCoverage != nil && f.ScanCoverage.Ignored > 0 {
		return fmt.Sprintf("CodeAtlas discovered %d file(s) without a registered parser; those files are not graph evidence", f.ScanCoverage.Ignored)
	}
	if f.GraphCommit == "" {
		return "graph has no commit metadata — freshness cannot be verified"
	}
	if !f.Verifiable {
		return "graph freshness could not be verified against a repository checkout"
	}
	if !f.StateVerifiable {
		return "graph file fingerprints are unavailable — repository state could not be verified"
	}
	if f.EntityIdentity == "" {
		return "graph uses a legacy entity identity scheme — rescan before relying on implementation guidance"
	}
	return ""
}

// PromptContext makes freshness state explicit to the model when a read-only
// question is answered from a graph that is usable but not fully verified.
// The model must see this state in-band; a stderr warning is not part of its
// evidence and cannot reliably constrain its answer.
func (f Freshness) PromptContext() string {
	availability := "available"
	if !f.Available {
		availability = "unavailable"
	}

	freshness := "current"
	if f.Stale {
		freshness = "stale"
	} else if !f.Verifiable {
		freshness = "unverified"
	}

	repository := "matches requested checkout"
	if f.RepositoryMismatch {
		repository = "does not match requested checkout"
	} else if f.GraphRepository == "" {
		repository = "not established"
	}

	scan := "complete"
	if f.Incomplete {
		scan = "incomplete"
	}

	state := "verified"
	if !f.StateVerifiable {
		state = "unverified"
	}

	identity := f.EntityIdentity
	if identity == "" {
		identity = "legacy or unavailable"
	}

	var b strings.Builder
	b.WriteString("CodeAtlas verification status (metadata, not repository content):\n")
	fmt.Fprintf(&b, "- graph availability: %s\n", availability)
	schema := f.SchemaVersion
	if schema == "" {
		schema = "legacy or unavailable"
	}
	schemaState := "current"
	if !f.SchemaCurrent {
		schemaState = "not current"
	}
	fmt.Fprintf(&b, "- graph schema: %s (%s)\n", schema, schemaState)
	fmt.Fprintf(&b, "- graph freshness: %s\n", freshness)
	fmt.Fprintf(&b, "- repository match: %s\n", repository)
	fmt.Fprintf(&b, "- scan: %s\n", scan)
	fmt.Fprintf(&b, "- repository state verification: %s\n", state)
	fmt.Fprintf(&b, "- entity identity: %s\n", identity)
	if f.GraphCommit != "" {
		fmt.Fprintf(&b, "- graph commit: %s\n", short(f.GraphCommit))
	}
	if f.RepoHead != "" {
		fmt.Fprintf(&b, "- repository HEAD: %s\n", short(f.RepoHead))
	}
	if f.ScanCoverage != nil {
		fmt.Fprintf(&b, "- scan coverage: discovered=%d parsed=%d reused=%d ignored=%d failed=%d\n",
			f.ScanCoverage.Discovered, f.ScanCoverage.Parsed, f.ScanCoverage.Reused,
			f.ScanCoverage.Ignored, f.ScanCoverage.Failed)
	}
	for i, warning := range f.ScanWarnings {
		if i == 5 {
			fmt.Fprintf(&b, "- scan warnings: %d additional warning(s) omitted\n", len(f.ScanWarnings)-i)
			break
		}
		fmt.Fprintf(&b, "- scan warning: %s\n", warning)
	}
	if warning := f.Warning(); warning != "" {
		fmt.Fprintf(&b, "- limitation: %s\n", warning)
	}
	b.WriteString("Use the query evidence below only with these verification limits; report the limitation when it affects the answer.")
	return b.String()
}

// BlocksImplementation reports whether an operation that may lead to code
// changes should refuse to proceed. Read-only questions may still use an
// incomplete or unverifiable graph, but generated implementation guidance
// must have a graph that is both available and tied to the requested checkout.
func (f Freshness) BlocksImplementation() bool {
	return !f.Available || f.Stale || f.Incomplete || f.RepositoryMismatch || f.Dirty || !f.Verifiable || !f.StateVerifiable || f.EntityIdentity == "" || f.SchemaVersion == "" || !f.SchemaCurrent
}

func CheckFreshness(a Runner, repoPath string) Freshness {
	meta := parseGraphMetadata(a)
	if !meta.Available {
		return Freshness{}
	}
	checkPath := repoPath
	if checkPath == "" {
		checkPath = meta.Repository
	}
	if jr, ok := a.(JSONRunner); ok && checkPath != "" {
		if status, ok := parseFreshness(jr, checkPath); ok {
			return status
		}
	}
	if repoPath == "" {
		repoPath = meta.Repository
	}
	head := ""
	if repoPath != "" {
		if _, err := os.Stat(repoPath); err == nil {
			head = repoHead(repoPath)
		}
	}
	f := Freshness{
		GraphCommit:     meta.Commit,
		RepoHead:        head,
		GraphRepository: meta.Repository,
		SchemaVersion:   meta.SchemaVersion,
		SchemaCurrent:   meta.SchemaVersion == CurrentSchemaVersion,
		EntityIdentity:  meta.EntityIdentity,
		ScanCoverage:    meta.ScanCoverage,
		ScanWarnings:    append([]string(nil), meta.ScanWarnings...),
		Incomplete:      !meta.ScanComplete,
		Available:       true,
	}
	if meta.Repository != "" && repoPath != "" && comparableRepoPath(meta.Repository) != "" && comparableRepoPath(repoPath) != "" {
		graphRoot := comparableRepoPath(meta.Repository)
		repoRoot := comparableRepoPath(repoPath)
		if graphRoot != repoRoot {
			f.RepositoryMismatch = true
		}
	}
	if meta.Commit != "" && head != "" {
		f.Verifiable = true
		f.Stale = meta.Commit != head
	}
	return f
}

type freshnessJSON struct {
	Available       bool          `json:"available"`
	SchemaVersion   string        `json:"schemaVersion"`
	SchemaCurrent   bool          `json:"schemaCurrent"`
	GraphRepository string        `json:"graphRepository"`
	Repository      string        `json:"repository"`
	GraphCommit     string        `json:"graphCommit"`
	RepoHead        string        `json:"repoHead"`
	EntityIdentity  string        `json:"entityIdentity"`
	ScanComplete    bool          `json:"scanComplete"`
	ScanCoverage    *ScanCoverage `json:"scanCoverage"`
	ScanWarnings    []string      `json:"scanWarnings"`
	RepositoryMatch bool          `json:"repositoryMatch"`
	Verifiable      bool          `json:"verifiable"`
	Stale           bool          `json:"stale"`
	Dirty           bool          `json:"dirty"`
	StateVerifiable bool          `json:"stateVerifiable"`
	ChangedFiles    []string      `json:"changedFiles"`
	NewFiles        []string      `json:"newFiles"`
	DeletedFiles    []string      `json:"deletedFiles"`
}

func parseFreshness(a JSONRunner, repoPath string) (Freshness, bool) {
	output, err := a.RunJSON("freshness", "--repo", repoPath)
	if err != nil {
		return Freshness{}, false
	}
	var raw map[string]any
	if json.Unmarshal([]byte(output), &raw) != nil {
		return Freshness{}, false
	}
	if _, exists := raw["stateVerifiable"]; !exists {
		return Freshness{}, false
	}
	var value freshnessJSON
	if json.Unmarshal([]byte(output), &value) != nil {
		return Freshness{}, false
	}
	return Freshness{
		GraphCommit:        value.GraphCommit,
		RepoHead:           value.RepoHead,
		GraphRepository:    value.GraphRepository,
		SchemaVersion:      value.SchemaVersion,
		SchemaCurrent:      value.SchemaCurrent || value.SchemaVersion == CurrentSchemaVersion,
		EntityIdentity:     value.EntityIdentity,
		ScanCoverage:       value.ScanCoverage,
		ScanWarnings:       append([]string(nil), value.ScanWarnings...),
		Stale:              value.Stale,
		Incomplete:         !value.ScanComplete,
		RepositoryMismatch: !value.RepositoryMatch,
		Dirty:              value.Dirty,
		StateVerifiable:    value.StateVerifiable,
		Verifiable:         value.Verifiable,
		Available:          value.Available,
	}, true
}

func existingRepoPath(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

func comparableRepoPath(path string) string {
	if path == "" || strings.Contains(path, "://") {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

type graphMetadata struct {
	Repository     string        `json:"repository"`
	Commit         string        `json:"commit"`
	SchemaVersion  string        `json:"schemaVersion"`
	EntityIdentity string        `json:"entityIdentity"`
	ScanComplete   bool          `json:"scanComplete"`
	ScanCoverage   *ScanCoverage `json:"scanCoverage"`
	ScanWarnings   []string      `json:"scanWarnings"`
	Available      bool          `json:"-"`
}

func parseGraphMetadata(a Runner) graphMetadata {
	if jr, ok := a.(JSONRunner); ok {
		if out, err := jr.RunJSON("stats"); err == nil {
			var meta graphMetadata
			if json.Unmarshal([]byte(out), &meta) == nil {
				meta.Available = true
				return meta
			}
		}
	}
	out, err := a.Run("stats")
	if err != nil {
		return graphMetadata{}
	}
	var meta graphMetadata
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "schema: codeatlas/"):
			meta.SchemaVersion = strings.TrimPrefix(line, "schema: codeatlas/")
		case strings.HasPrefix(line, "repository: "):
			meta.Repository = strings.TrimPrefix(line, "repository: ")
		case strings.HasPrefix(line, "commit: "):
			meta.Commit = strings.TrimPrefix(line, "commit: ")
		case strings.HasPrefix(line, "entity identity: "):
			meta.EntityIdentity = strings.TrimPrefix(line, "entity identity: ")
		case strings.HasPrefix(line, "scan coverage: "):
			var coverage ScanCoverage
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "scan coverage: "), "discovered=%d parsed=%d reused=%d ignored=%d failed=%d",
				&coverage.Discovered, &coverage.Parsed, &coverage.Reused, &coverage.Ignored, &coverage.Failed); err == nil {
				meta.ScanCoverage = &coverage
			}
		case strings.HasPrefix(line, "scan warning: "):
			meta.ScanWarnings = append(meta.ScanWarnings, strings.TrimPrefix(line, "scan warning: "))
		case strings.TrimSpace(line) == "scan: complete":
			meta.ScanComplete = true
		}
	}
	if out != "" {
		meta.Available = true
		return meta
	}
	return graphMetadata{}
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
