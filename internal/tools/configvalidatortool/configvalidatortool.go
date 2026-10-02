package configvalidatortool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

var configValidatorDescription = `Validate ~/.a1/config.yaml and environment variables.

Reports missing required fields, invalid values, and potential misconfigurations
so the agent can fix setup issues before they cause runtime failures.`

// ConfigValidatorTool returns the config/env validator tool definition + handler.
func ConfigValidatorTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "config_validate",
			Description: configValidatorDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Config file path. Example: ~/.a1/config.yaml",
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in configValidatorInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				return "config_validate"
			}
			return fmt.Sprintf("config_validate %s", p)
		},
		Run: runConfigValidate,
	}
}

type configValidatorInput struct {
	Path string `json:"path,omitempty"`
}

type validationIssue struct {
	Severity string
	Message  string
}

func runConfigValidate(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in configValidatorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse config_validate arguments: %w", err)
	}

	path := strings.TrimSpace(in.Path)
	if path == "" {
		proj := project.GetDefaultProject()
		if proj != nil {
			path = proj.Global().ConfigFile()
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return tooldef.Result{}, fmt.Errorf("config_validate: cannot determine home dir: %w", err)
			}
			path = filepath.Join(home, ".a1", "config.yaml")
		}
	}

	var issues []validationIssue

	// Check file existence.
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			issues = append(
				issues,
				validationIssue{Severity: "error", Message: fmt.Sprintf("config file not found: %s", path)},
			)
			return renderValidationResult(issues), nil
		}
		return tooldef.Result{}, fmt.Errorf("config_validate: stat %s: %w", path, err)
	}
	if info.IsDir() {
		issues = append(
			issues,
			validationIssue{Severity: "error", Message: fmt.Sprintf("config path is a directory: %s", path)},
		)
		return renderValidationResult(issues), nil
	}

	// Try to load config through the project loader.
	proj, err := project.Discover("")
	if err != nil {
		issues = append(
			issues,
			validationIssue{Severity: "warn", Message: fmt.Sprintf("project discover failed: %v", err)},
		)
		return renderValidationResult(issues), nil
	}
	if err := proj.LoadConfig(); err != nil {
		issues = append(issues, validationIssue{Severity: "error", Message: fmt.Sprintf("config parse error: %v", err)})
		return renderValidationResult(issues), nil
	}
	cfg := proj.Config()

	// Validate models.
	if len(cfg.Models) == 0 {
		issues = append(issues, validationIssue{Severity: "error", Message: "no models configured"})
	} else {
		seenDefault := false
		for i, m := range cfg.Models {
			if m.Name == "" {
				issues = append(
					issues,
					validationIssue{Severity: "error", Message: fmt.Sprintf("model[%d] missing name", i)},
				)
			}
			if m.APIKey == "" {
				issues = append(
					issues,
					validationIssue{Severity: "error", Message: fmt.Sprintf("model %q missing api_key", m.Name)},
				)
			}
			if m.Name == cfg.DefaultModel {
				if seenDefault {
					issues = append(
						issues,
						validationIssue{
							Severity: "warn",
							Message:  fmt.Sprintf("multiple default models configured; using %q", cfg.DefaultModel),
						},
					)
				}
				seenDefault = true
			}
		}
		if cfg.DefaultModel != "" {
			found := false
			for _, m := range cfg.Models {
				if m.Name == cfg.DefaultModel {
					found = true
					break
				}
			}
			if !found {
				issues = append(
					issues,
					validationIssue{
						Severity: "warn",
						Message:  fmt.Sprintf("default_model %q does not match any configured model", cfg.DefaultModel),
					},
				)
			}
		}
	}

	// Validate permissions.
	mode := string(cfg.Permissions.Mode)
	if mode != "" && mode != string(permission.ModeInteractive) && mode != string(permission.ModeReadonly) &&
		mode != string(permission.ModeAutopilot) && mode != string(permission.ModeHeadlessStrict) {
		issues = append(
			issues,
			validationIssue{Severity: "error", Message: fmt.Sprintf("invalid permissions.mode: %q", mode)},
		)
	}
	if cfg.Permissions.BashDefault != permission.Allow && cfg.Permissions.BashDefault != permission.Deny &&
		cfg.Permissions.BashDefault != permission.Ask {
		issues = append(
			issues,
			validationIssue{
				Severity: "error",
				Message:  fmt.Sprintf("invalid permissions.bash.default: %v", cfg.Permissions.BashDefault),
			},
		)
	}

	// Validate env overrides.
	if os.Getenv("PHI_MODEL") != "" && cfg.DefaultModel != "" && os.Getenv("PHI_MODEL") != cfg.DefaultModel {
		issues = append(issues, validationIssue{Severity: "info", Message: "PHI_MODEL overrides default_model"})
	}
	if os.Getenv("PHI_API_KEY") != "" {
		issues = append(issues, validationIssue{Severity: "info", Message: "PHI_API_KEY is set"})
	}

	if len(issues) == 0 {
		return tooldef.Result{Content: "Config is valid.\n", Detail: "0 issues", Output: "Config is valid.\n"}, nil
	}

	return renderValidationResult(issues), nil
}

func renderValidationResult(issues []validationIssue) tooldef.Result {
	var sb strings.Builder
	sb.WriteString("Config validation results:\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for _, issue := range issues {
		prefix := "info"
		switch issue.Severity {
		case "error":
			prefix = "error"
		case "warn":
			prefix = "warn"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", prefix, issue.Message))
	}
	sb.WriteString("\n")
	errCount := 0
	for _, i := range issues {
		if i.Severity == "error" {
			errCount++
		}
	}
	detail := fmt.Sprintf("%d issues (%d errors)", len(issues), errCount)
	return tooldef.Result{Content: sb.String(), Detail: detail, Output: sb.String()}
}
