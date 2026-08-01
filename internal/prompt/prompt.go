package prompt

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.tmpl"))

type AskData struct {
	Question    string
	AtlasOutput string
}

type SolveData struct {
	JiraText    string
	AtlasData   string
	Conventions string
	StyleCode   string
	APITypes    string
}

type ControllerEntry struct {
	ID   string
	File string
	Role string
}

type FrameworkInfo struct {
	RelPath    string
	Components string
	Count      int
}

type ClaudeData struct {
	JiraText           string
	AtlasData          string
	Conventions        string
	StyleCode          string
	Controllers        []ControllerEntry
	WorkloadController string
	RepoFiles          string
	Framework          *FrameworkInfo
	APITypes           string
}

type GenerateData struct {
	Description string
	AtlasData   string
	Conventions string
	StyleCode   string
}

func BuildAsk(question, atlasOutput string) string {
	return execute("ask.tmpl", AskData{
		Question:    question,
		AtlasOutput: atlasOutput,
	})
}

func BuildSolve(jiraText, atlasData, conventions, styleCode, apiTypes string) string {
	return execute("solve.tmpl", SolveData{
		JiraText:    jiraText,
		AtlasData:   atlasData,
		Conventions: conventions,
		StyleCode:   styleCode,
		APITypes:    apiTypes,
	})
}

func BuildGenerate(description, atlasData, styleCode, conventions string) string {
	return execute("generate.tmpl", GenerateData{
		Description: description,
		AtlasData:   atlasData,
		Conventions: conventions,
		StyleCode:   styleCode,
	})
}

func BuildClaude(jiraText, atlasData, conventions, styleCode, repoFiles, apiTypes string, framework *FrameworkInfo, controllers []ControllerEntry) string {
	workload := ""
	for _, c := range controllers {
		if c.Role == "workload" {
			workload = c.ID
			break
		}
	}
	return execute("claude.tmpl", ClaudeData{
		JiraText:           jiraText,
		AtlasData:          atlasData,
		Conventions:        conventions,
		StyleCode:          styleCode,
		Controllers:        controllers,
		WorkloadController: workload,
		RepoFiles:          repoFiles,
		Framework:          framework,
		APITypes:           apiTypes,
	})
}

func execute(name string, data any) string {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return ""
	}
	return buf.String()
}
