# Go CLI Development

## Frameworks

| Framework | Best for | Notes |
|---|---|---|
| **cobra** | Most Go CLIs (kubectl, docker, hugo, gh) | Subcommands, completions, help |
| **urfave/cli** | Lighter alternative | Simpler API |
| **bubbletea** | TUI apps | Elm-style, interactive |
| **promptui** | Interactive prompts | Searchable select, validation |

## Cobra (recommended)

```go
// cmd/root.go
package cmd

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var (
    cfgFile string
    verbose bool
)

var rootCmd = &cobra.Command{
    Use:   "mycli",
    Short: "My awesome CLI",
    Long:  `A longer description of your CLI application.`,
    Version: "1.0.0",
    SilenceUsage: true,   // don't print usage on RunE error
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

func init() {
    cobra.OnInitialize(initConfig)

    rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
    rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

    _ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        home, err := os.UserHomeDir()
        cobra.CheckErr(err)
        viper.AddConfigPath(home + "/.config/mycli")
        viper.AddConfigPath(".")
        viper.SetConfigType("yaml")
        viper.SetConfigName("config")
    }
    viper.AutomaticEnv()
    viper.SetEnvPrefix("MYCLI")
    if err := viper.ReadInConfig(); err == nil {
        fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
    }
}
```

```go
// cmd/deploy.go
package cmd

import (
    "fmt"

    "github.com/spf13/cobra"
)

var dryRun bool

var deployCmd = &cobra.Command{
    Use:   "deploy [environment]",
    Short: "Deploy to environment",
    Long:  `Deploy the application to the specified environment.`,
    Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
    ValidArgs: []string{"dev", "staging", "prod"},
    RunE: func(cmd *cobra.Command, args []string) error {
        env := args[0]
        if dryRun {
            fmt.Fprintf(os.Stderr, "Would deploy to %s\n", env)
            return nil
        }
        return deploy(env)
    },
}

func init() {
    deployCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Preview only")
    rootCmd.AddCommand(deployCmd)
}

func deploy(env string) error {
    fmt.Printf("Deploying to %s...\n", env)
    // ... actual deployment
    return nil
}
```

```go
// cmd/config.go (subcommand group)
package cmd

import (
    "fmt"

    "github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
    Use:   "config",
    Short: "Manage configuration",
}

var configGetCmd = &cobra.Command{
    Use:   "get [key]",
    Short: "Get a config value",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Println(viper.GetString(args[0]))
        return nil
    },
}

var configSetCmd = &cobra.Command{
    Use:   "set [key] [value]",
    Short: "Set a config value",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        viper.Set(args[0], args[1])
        return viper.WriteConfig()
    },
}

func init() {
    configCmd.AddCommand(configGetCmd)
    configCmd.AddCommand(configSetCmd)
    rootCmd.AddCommand(configCmd)
}
```

```go
// main.go
package main

import "mycli/cmd"

func main() {
    cmd.Execute()
}
```

### Cobra features

- **`Use: "deploy [environment]"`** — defines usage syntax
- **`Short:`, `Long:`** — help text (`Short` for parent listing, `Long` for `--help`)
- **`Args: cobra.ExactArgs(1)` / `cobra.NoArgs` / `cobra.MaximumNArgs(2)`** — arg count validation
- **`ValidArgs: []string{...}`** — restrict positional args to enum
- **`RunE: func() error`** — return error instead of `os.Exit` (cobra handles)
- **`PersistentFlags()`** — inherited by subcommands
- **`Flags()`** — local to this command
- **`PreRunE`, `PostRunE`** — hooks before/after Run
- **`SilenceUsage: true`** — don't print usage on error (cleaner UX)

## Viper (configuration)

```go
package config

import (
    "fmt"
    "github.com/spf13/viper"
)

type Config struct {
    Environment string `mapstructure:"environment"`
    Timeout     int    `mapstructure:"timeout"`
    Verbose     bool   `mapstructure:"verbose"`
    API         APIConfig `mapstructure:"api"`
}

type APIConfig struct {
    Endpoint string `mapstructure:"endpoint"`
    Token    string `mapstructure:"token"`
}

func Load() (*Config, error) {
    viper.SetDefault("environment", "development")
    viper.SetDefault("timeout", 30)
    viper.SetDefault("verbose", false)

    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath("/etc/mycli/")
    viper.AddConfigPath("$HOME/.config/mycli")
    viper.AddConfigPath(".")

    viper.SetEnvPrefix("MYCLI")
    viper.AutomaticEnv()

    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, fmt.Errorf("failed to read config: %w", err)
        }
    }

    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("failed to unmarshal config: %w", err)
    }
    return &cfg, nil
}
```

## Bubble Tea (TUI)

Elm-architecture TUI for interactive CLIs.

```go
package main

import (
    "fmt"
    "os"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

type model struct {
    choices  []string
    cursor   int
    selected map[int]struct{}
}

func initialModel() model {
    return model{
        choices:  []string{"TypeScript", "ESLint", "Prettier", "Jest"},
        selected: make(map[int]struct{}),
    }
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "ctrl+c", "q":
            return m, tea.Quit
        case "up", "k":
            if m.cursor > 0 { m.cursor-- }
        case "down", "j":
            if m.cursor < len(m.choices)-1 { m.cursor++ }
        case " ":
            if _, ok := m.selected[m.cursor]; ok {
                delete(m.selected, m.cursor)
            } else {
                m.selected[m.cursor] = struct{}{}
            }
        case "enter":
            return m, tea.Quit
        }
    }
    return m, nil
}

func (m model) View() string {
    s := "Select features:\n\n"
    for i, choice := range m.choices {
        cursor := " "
        if m.cursor == i { cursor = ">" }
        checked := " "
        if _, ok := m.selected[i]; ok { checked = "x" }
        s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
    }
    s += "\nPress space to select, enter to confirm, q to quit.\n"
    return s
}

func main() {
    p := tea.NewProgram(initialModel())
    if _, err := p.Run(); err != nil {
        fmt.Printf("Error: %v", err)
        os.Exit(1)
    }
}
```

Other Charm libraries: `lipgloss` (styles), `bubbles` (components: text input, list, viewport, spinner), `glamour` (markdown rendering).

## Colored Output

```go
package main

import (
    "os"

    "github.com/fatih/color"
)

func main() {
    // Auto-disables color when not TTY or NO_COLOR set
    color.Blue("Info: starting")
    color.Green("Success: done")
    color.Yellow("Warning: deprecated flag")
    color.Red("Error: failed")

    // Custom style
    success := color.New(color.FgGreen, color.Bold).SprintFunc()
    fmt.Printf("%s deployed\n", success("✔"))

    // Force-disable in CI
    if os.Getenv("CI") != "" || os.Getenv("NO_COLOR") != "" {
        color.NoColor = true
    }
}
```

Or use `lipgloss` (more modern):

```go
import "github.com/charmbracelet/lipgloss"

var (
    successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
    errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
)

fmt.Println(successStyle.Render("✔ Deploy complete"))
fmt.Println(errorStyle.Render("✖ Deploy failed"))
```

## Progress + Spinners

```go
package main

import (
    "time"
    "github.com/schollz/progressbar/v3"
    "github.com/briandowns/spinner"
)

func progressBar() {
    bar := progressbar.Default(100, "Downloading")
    for i := 0; i < 100; i++ {
        bar.Add(1)
        time.Sleep(40 * time.Millisecond)
    }
}

func spinnerExample() {
    s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
    s.Suffix = " Installing dependencies..."
    s.Start()
    time.Sleep(4 * time.Second)
    s.Stop()
    fmt.Println("✓ Done!")
}
```

## Error Handling + SIGINT

```go
package main

import (
    "errors"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/spf13/cobra"
)

func main() {
    // SIGINT handler
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-sig
        fmt.Fprintln(os.Stderr, "\nOperation cancelled")
        os.Exit(130)
    }()

    if err := rootCmd.Execute(); err != nil {
        // Don't print usage on RunE errors (silenced above)
        var exitErr *ExitError
        if errors.As(err, &exitErr) {
            os.Exit(exitErr.Code)
        }
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

// Custom exit-error type for explicit codes
type ExitError struct {
    Code int
    Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

// Usage: return &ExitError{Code: 2, Msg: "invalid environment"}
```

## Testing CLIs

```go
package cmd

import (
    "bytes"
    "testing"

    "github.com/spf13/cobra"
    "github.com/stretchr/testify/assert"
)

func TestDeployDryRun(t *testing.T) {
    cmd := &cobra.Command{Use: "test"}
    cmd.AddCommand(deployCmd)

    out := &bytes.Buffer{}
    cmd.SetOut(out)
    cmd.SetErr(out)
    cmd.SetArgs([]string{"deploy", "prod", "--dry-run"})

    err := cmd.Execute()
    assert.NoError(t, err)
    assert.Contains(t, out.String(), "Would deploy to prod")
}

func TestInvalidEnvExits2(t *testing.T) {
    cmd := &cobra.Command{Use: "test"}
    cmd.AddCommand(deployCmd)

    cmd.SetArgs([]string{"deploy", "invalid"})
    err := cmd.Execute()
    assert.Error(t, err)
}
```

## Build & Distribution

### Makefile

```makefile
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION) -s -w"

.PHONY: build
build:
	go build $(LDFLAGS) -o bin/mycli main.go

.PHONY: install
install:
	go install $(LDFLAGS)

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: release
release:
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o bin/mycli-linux-amd64
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o bin/mycli-linux-arm64
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o bin/mycli-darwin-amd64
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o bin/mycli-darwin-arm64
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/mycli-windows-amd64.exe
```

### `.goreleaser.yml` (recommended)

```yaml
project_name: mycli
before:
  hooks:
    - go mod tidy
builds:
  - env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}
archives:
  - format_overrides:
      - goos: windows
        formats: [zip]
brews:
  - repository:
      owner: myorg
      name: homebrew-tap
    homepage: https://github.com/myorg/mycli
    description: My awesome CLI
scoops:
  - repository:
      owner: myorg
      name: scoop-bucket
    homepage: https://github.com/myorg/mycli
    description: My awesome CLI
checksum:
  name_template: 'checksums.txt'
snapshot:
  name_template: "{{ incpatch .Version }}-next"
changelog:
  sort: asc
  filters:
    exclude: ['^docs:', '^test:', '^chore:']
```

```bash
# Release
goreleaser release --clean
```

GoReleaser publishes:
- Cross-compiled binaries to GitHub Releases
- Homebrew tap formula
- Scoop manifest (Windows)
- AUR package (optional)
- Docker image (optional)

## Shell Completions (Cobra built-in)

```bash
# Generate completion script for each shell
mycli completion bash > /etc/bash_completion.d/mycli
mycli completion zsh  > "${fpath[1]}/_mycli"
mycli completion fish > ~/.config/fish/completions/mycli.fish
mycli completion powershell | Out-String | Invoke-Expression
```

Cobra auto-generates a `completion` subcommand. Just call it and pipe to the appropriate file.

## Startup Time

Go CLIs are typically <20ms cold-start (static binary, no runtime). To keep it that way:

1. **Avoid heavy `init()` functions** — they run on every invocation
2. **Lazy-load optional deps** — use subcommand-scoped imports
3. **Use `-ldflags "-s -w"`** to strip debug info (smaller binary, faster start)
4. **Avoid cobra's `cobra.OnInitialize` for heavy work** — runs even for `--version`
5. **Benchmark**: `hyperfine --warmup 3 './mycli --version'`

## Common Pitfalls

1. **`RunE` returns nil on error** — cobra exits 0, breaks shell scripting.
2. **`fmt.Println` for errors** — goes to stdout. Use `fmt.Fprintln(os.Stderr, ...)`.
3. **Forgetting `cobra.OnInitialize(initConfig)`** — config never loaded.
4. **Heavy `init()`** — slow startup for `--version`.
5. **No `SilenceUsage: true`** — usage printed on every error, noisy.
6. **`os.Exit()` inside `RunE`** — skips deferred cleanups. Return error instead.
7. **No SIGINT handler** — Ctrl+C leaves temp files / half-finished state.
8. **Hardcoded `/home/user` paths** — use `os.UserHomeDir()`.
9. **Not setting `Version:`** — `--version` doesn't work.
10. **Forgetting `ValidArgs`** — no shell completion for positional args.
