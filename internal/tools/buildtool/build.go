package buildtool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/llm"
)

const (
	buildDefaultLimit = 50
)

var buildDescription = `Build system integration.

Understands make targets, cmake configuration, pkg-config dependencies,
meson options, cargo metadata, go build tags, and workspace manifests.
Returns targets, dependencies, build order, and relevant source files.`

// BuildTool returns the build system integration tool definition + handler.
func BuildTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "build",
			Description: buildDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Project root to inspect. Example: .",
					},
					"limit": llm.Object{
						"type":        "integer",
								"description": fmt.Sprintf("Maximum results to return. Example: 20 (default: %d)", buildDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in buildInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("build %s", p)
		},
		Run: runBuild,
	}
}

type buildInput struct {
	Path  string `json:"path,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type buildTarget struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Source      string   `json:"source"`
	DependsOn   []string `json:"depends_on"`
	Command     string   `json:"command"`
	Description string   `json:"description"`
}

func runBuild(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in buildInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse build arguments: %w", err)
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = "."
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}
	if !info.IsDir() {
		return tooldef.Result{}, fmt.Errorf("path is not a directory: %s", searchPath)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = buildDefaultLimit
	}

	targets, err := inspectBuild(searchPath, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(targets) == 0 {
		return tooldef.Result{Content: "No build targets found", Detail: "0 targets", Output: "No build targets found"}, nil
	}

	content := renderBuildResults(ctx, targets)
	detail := fmt.Sprintf("%d build targets", len(targets))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func inspectBuild(root string, limit int) ([]buildTarget, error) {
	var targets []buildTarget
	manifest := detectManifest(root)
	switch manifest {
	case "makefile":
		targets = append(targets, inspectMakefile(root)...)
	case "cmake":
		targets = append(targets, inspectCMake(root)...)
	case "meson":
		targets = append(targets, inspectMeson(root)...)
	case "cargo":
		targets = append(targets, inspectCargo(root)...)
	case "go":
		targets = append(targets, inspectGo(root)...)
	case "npm":
		targets = append(targets, inspectNpm(root)...)
	case "gradle":
		targets = append(targets, inspectGradle(root)...)
	}
	if len(targets) > limit {
		targets = targets[:limit]
	}
	return targets, nil
}

func detectManifest(root string) string {
	names := map[string]bool{
		"Makefile": false, "makefile": false, "GNUmakefile": false,
		"CMakeLists.txt": false,
		"meson.build": false,
		"meson_options.txt": false,
		"Cargo.toml": false,
		"go.mod": false,
		"package.json": false,
		"build.gradle": false,
		"build.gradle.kts": false,
	}
	files, _ := os.ReadDir(root)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if _, ok := names[f.Name()]; ok {
			return manifestForFile(f.Name())
		}
	}
	return ""
}

func manifestForFile(name string) string {
	switch name {
	case "Makefile", "makefile", "GNUmakefile":
		return "makefile"
	case "CMakeLists.txt":
		return "cmake"
	case "meson.build", "meson_options.txt":
		return "meson"
	case "Cargo.toml":
		return "cargo"
	case "go.mod":
		return "go"
	case "package.json":
		return "npm"
	case "build.gradle", "build.gradle.kts":
		return "gradle"
	}
	return ""
}

func inspectMakefile(root string) []buildTarget {
	path := filepath.Join(root, "Makefile")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(root, "makefile")
	}
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(root, "GNUmakefile")
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	var targets []buildTarget
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 && !strings.Contains(line, "=") {
			name := strings.TrimSpace(line[:idx])
			rest := strings.TrimSpace(line[idx+1:])
			desc := ""
			if d := strings.TrimPrefix(line, ".PHONY:"); d != line {
				desc = "phony"
			}
			targets = append(targets, buildTarget{
				Name:      name,
				Kind:      "make",
				Source:    filepath.Base(path),
				DependsOn: splitDeps(rest),
				Command:   rest,
				Description: desc,
			})
		}
	}
	return targets
}

func inspectCMake(root string) []buildTarget {
	path := filepath.Join(root, "CMakeLists.txt")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	var targets []buildTarget
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "add_executable") && !strings.HasPrefix(line, "add_library") {
			continue
		}
		open := strings.Index(line, "(")
		close := strings.Index(line, ")")
		if open < 0 || close < 0 || close <= open {
			continue
		}
		inner := strings.Split(line[open+1:close], " ")
		if len(inner) == 0 {
			continue
		}
		name := inner[0]
		deps := []string{}
		if len(inner) > 1 {
			deps = inner[1:]
		}
		targets = append(targets, buildTarget{
			Name:      name,
			Kind:      "cmake",
			Source:    filepath.Base(path),
			DependsOn: deps,
			Command:   line,
			Description: kindForCMake(line),
		})
	}
	return targets
}

func inspectMeson(root string) []buildTarget {
	path := filepath.Join(root, "meson.build")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	var targets []buildTarget
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "executable") && !strings.HasPrefix(line, "library") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[1]
		deps := []string{}
		if len(parts) > 2 {
			deps = parts[2:]
		}
		targets = append(targets, buildTarget{
			Name:      name,
			Kind:      "meson",
			Source:    filepath.Base(path),
			DependsOn: deps,
			Command:   line,
			Description: kindForMeson(line),
		})
	}
	return targets
}

func inspectCargo(root string) []buildTarget {
	path := filepath.Join(root, "Cargo.toml")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	var targets []buildTarget
	inSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[[bin]]") || strings.HasPrefix(line, "[[lib]]") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if !strings.HasPrefix(line, "name") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, "\"'")
		if val == "" {
			continue
		}
		targets = append(targets, buildTarget{
			Name:      val,
			Kind:      "cargo",
			Source:    filepath.Base(path),
			DependsOn: []string{},
			Command:   line,
			Description: "cargo target",
		})
		inSection = false
	}
	return targets
}

func inspectGo(root string) []buildTarget {
	path := filepath.Join(root, "go.mod")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	var targets []buildTarget
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			targets = append(targets, buildTarget{
				Name:      "go build",
				Kind:      "go",
				Source:    filepath.Base(path),
				DependsOn: []string{},
				Command:   line,
				Description: "module build",
			})
			break
		}
	}
	return targets
}

func inspectNpm(root string) []buildTarget {
	path := filepath.Join(root, "package.json")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	b, _ := os.ReadFile(path)
	text := string(b)
	var targets []buildTarget
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "\"") || !strings.Contains(line, "\":") {
			continue
		}
		key := strings.Trim(line, "\"")
		if idx := strings.Index(key, "\""); idx > 0 {
			key = key[:idx]
		}
		if key == "" || strings.Contains(key, " ") {
			continue
		}
		targets = append(targets, buildTarget{
			Name:      key,
			Kind:      "npm",
			Source:    filepath.Base(path),
			DependsOn: []string{},
			Command:   line,
			Description: "npm script",
		})
	}
	return targets
}

func inspectGradle(root string) []buildTarget {
	path := filepath.Join(root, "build.gradle")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(root, "build.gradle.kts")
		if _, err := os.Stat(path); err != nil {
			return nil
		}
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	var targets []buildTarget
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "task ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[len(parts)-1]
		if strings.HasSuffix(name, "(") {
			name = strings.TrimSuffix(name, "(")
		}
		targets = append(targets, buildTarget{
			Name:      name,
			Kind:      "gradle",
			Source:    filepath.Base(path),
			DependsOn: []string{},
			Command:   line,
			Description: "gradle task",
		})
	}
	return targets
}

func kindForCMake(line string) string {
	if strings.HasPrefix(line, "add_executable") {
		return "executable"
	}
	if strings.HasPrefix(line, "add_library") {
		return "library"
	}
	return "target"
}

func kindForMeson(line string) string {
	if strings.HasPrefix(line, "executable") {
		return "executable"
	}
	if strings.HasPrefix(line, "library") {
		return "library"
	}
	return "target"
}

func splitDeps(deps string) []string {
	deps = strings.TrimSpace(deps)
	if deps == "" {
		return []string{}
	}
	fields := strings.Fields(deps)
	var out []string
	for _, f := range fields {
		f = strings.Trim(f, " ")
		if f == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func renderBuildResults(ctx context.Context, targets []buildTarget) string {
	var sb strings.Builder
	for _, t := range targets {
		sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\n", t.Name, t.Kind, t.Source, strings.Join(t.DependsOn, ", "), t.Description, t.Name))
	}
	return sb.String()
}

func sortTargets(targets []buildTarget) {
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
}
