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
}

type ClaudeData struct {
	JiraText    string
	AtlasData   string
	Conventions string
	StyleCode   string
	Controllers []string
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

func BuildSolve(jiraText, atlasData, conventions, styleCode string) string {
	return execute("solve.tmpl", SolveData{
		JiraText:    jiraText,
		AtlasData:   atlasData,
		Conventions: conventions,
		StyleCode:   styleCode,
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

func BuildClaude(jiraText, atlasData, conventions, styleCode string, controllers []string) string {
	return execute("claude.tmpl", ClaudeData{
		JiraText:    jiraText,
		AtlasData:   atlasData,
		Conventions: conventions,
		StyleCode:   styleCode,
		Controllers: controllers,
	})
}

func execute(name string, data any) string {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return ""
	}
	return buf.String()
}
