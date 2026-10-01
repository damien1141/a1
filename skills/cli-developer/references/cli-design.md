# CLI Design Patterns

## Command Hierarchy

```
mycli                            # Root command
├── init [name] [options]        # Simple command
├── config                       # Subcommand group
│   ├── get <key>
│   ├── set <key> <value>
│   └── list
├── deploy <environment> [opts]  # Command with positional arg
│   ├── --dry-run
│   ├── --force
│   └── --config <file>
└── plugins                      # Plugin subcommands
    ├── install <name>
    ├── list
    └── remove <name>
```

### Principles

1. **Nouns before verbs** for resources: `config get`, not `get-config`
2. **Verb first** for actions: `deploy production`, not `production deploy`
3. **Limit nesting** — 2-3 levels max; deeper = hard to remember
4. **Provide aliases** for common commands: `deploy` → `dep`, `ls` → `list`
5. **`--help` everywhere** — root, every subcommand, every subcommand group

## Flag Conventions

### Boolean flags (presence = true)

```bash
mycli deploy --force --dry-run
```

### Short + long forms

```bash
mycli -v                    # short
mycli --verbose             # long
mycli -c config.yml         # short with value
mycli --config config.yml   # long with value
mycli --config=config.yml   # equals form
```

### Required vs optional

```bash
mycli deploy <env>           # Positional, required
mycli deploy --env production # Flag, optional (with default or prompt)
```

### Variadic args

```bash
mycli install pkg1 pkg2 pkg3
mycli --exclude node_modules --exclude .git --exclude dist
```

### Standard flags every CLI should have

| Flag | Purpose |
|---|---|
| `-h, --help` | Show help |
| `-v, --version` | Show version |
| `--verbose` | Detailed output |
| `--quiet, -q` | Suppress non-error output |
| `--config <file>` | Override config file path |
| `--no-color` | Disable color (also honor `NO_COLOR` env) |
| `--dry-run` | Preview without executing |
| `--yes, -y` | Skip confirmation prompts |

## Exit Codes (POSIX)

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error |
| 2 | Misuse (bad arguments) |
| 64-78 | sysexits.h conventions (optional) |
| 126 | Command found but not executable |
| 127 | Command not found |
| 128 + N | Killed by signal N (130 = SIGINT) |

### Language implementations

**Node:**
```javascript
process.exit(0);   // success
process.exit(1);   // error
process.exit(2);   // misuse
process.exit(130); // SIGINT
```

**Python:**
```python
import sys
sys.exit(0)   # success
sys.exit(1)   # error
sys.exit(2)   # misuse
sys.exit(130) # SIGINT
```

**Go:**
```go
os.Exit(0)
os.Exit(1)
os.Exit(2)
os.Exit(130)
```

**Rust:**
```rust
std::process::exit(0);
std::process::exit(1);
std::process::exit(2);
std::process::exit(130);
```

Always set explicit exit codes — don't rely on framework defaults.

## Configuration Layers

Priority (highest → lowest):

1. **CLI flags** — explicit user intent (wins)
2. **Environment variables** — runtime context (`MYCLI_*)
3. **Project config** — `./.myclirc`, `./mycli.config.yml` (in repo)
4. **User config** — `~/.config/mycli/config.yml` (XDG)
5. **System config** — `/etc/mycli/config.yml`
6. **Built-in defaults** — hardcoded sensible values

```javascript
// Node — layered config resolution
import { readFileSync, existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

function loadConfig(explicitPath) {
  const layers = [
    { name: 'defaults', data: defaultConfig() },
    { name: 'system',   path: '/etc/mycli/config.yml' },
    { name: 'user',     path: join(homedir(), '.config', 'mycli', 'config.yml') },
    { name: 'project',  path: join(process.cwd(), '.myclirc') },
    { name: 'env',      data: envConfig() },   // MYCLI_*
    { name: 'flags',    data: parsedFlags },   // from commander/yargs
  ];

  return layers.reduce((acc, layer) => {
    const data = layer.path ? loadYamlIfExists(layer.path) : layer.data;
    return { ...acc, ...data };
  }, {});
}
```

```go
// Go — Viper handles this natively
viper.SetDefault("environment", "development")
viper.SetConfigName("config")
viper.AddConfigPath("/etc/mycli/")
viper.AddConfigPath("$HOME/.config/mycli")
viper.AddConfigPath(".")
viper.SetEnvPrefix("MYCLI")
viper.AutomaticEnv()
viper.ReadInConfig()
viper.BindPFlags(rootCmd.PersistentFlags())  // CLI flags override all
```

## Environment Variables

### Conventions

- Prefix with app name: `MYCLI_*`
- Uppercase with underscores: `MYCLI_DATABASE_URL`
- Booleans as `1`/`0`, `true`/`false`, `yes`/`no`
- Lists as comma-separated or repeated: `MYCLI_ENDPOINTS=a,b,c` or `MYCLI_ENDPOINT_0=a MYCLI_ENDPOINT_1=b`
- Secrets via env (12-factor): `MYCLI_API_KEY=...`

```python
import os

class Config:
    DATABASE_URL: str = os.environ.get("MYCLI_DATABASE_URL", "postgres://localhost/myapp")
    LOG_LEVEL: str = os.environ.get("MYCLI_LOG_LEVEL", "info")
    TIMEOUT: int = int(os.environ.get("MYCLI_TIMEOUT", "30"))
```

## Error Handling

### Good error pattern

```
[Context] → [Problem] → [Solution]

✗ Error: Config file not found at /path/to/config.yml

Searched locations:
  • ./mycli.config.yml
  • ~/.config/mycli/config.yml
  • /etc/mycli/config.yml

Solutions:
  • Run 'mycli init' to create a config file
  • Use --config to specify a different location
  • Check file permissions
```

### Bad error

```
ENOENT
```

### Guidelines

| Do | Don't |
|---|---|
| Be specific ("Port 3000 in use") | Be vague ("Something went wrong") |
| Show context ("in config.yml line 42") | Show stack traces to users |
| Suggest solutions ("Try `mycli fix`") | Use jargon ("EACCES: permission denied") |
| Use plain language | Print to stdout |
| Provide exit code | Exit 0 on error |

## Interactive vs Non-Interactive

Detect CI / non-TTY and skip prompts:

```javascript
const isInteractive = !process.env.CI && process.stdin.isTTY;

if (!options.environment) {
  if (isInteractive) {
    options.environment = await select({
      message: 'Select environment:',
      choices: ['dev', 'staging', 'prod'],
    });
  } else {
    console.error('--environment is required in non-interactive mode');
    process.exit(2);
  }
}
```

```python
import sys

is_interactive = sys.stdin.isatty() and not os.environ.get("CI")

if not environment:
    if is_interactive:
        environment = questionary.select("Select environment:", choices=["dev", "staging", "prod"]).ask()
    else:
        typer.echo("--environment is required in non-interactive mode", err=True)
        raise typer.Exit(2)
```

## Plugin Architecture

For extensible CLIs (kubectl, gh, terraform plugins).

```
mycli/
├── core/                       # core functionality
├── plugins/
│   ├── aws/
│   │   └── index.js            # exports hooks + commands
│   └── github/
│       └── index.js
└── plugin-loader.js
```

Discovery:
1. `~/.mycli/plugins/*`
2. `node_modules/mycli-plugin-*` (npm convention)
3. `MYCLI_PLUGIN_PATH` env var

Plugin contract:

```javascript
// plugins/aws/index.js
export const name = 'aws';
export const description = 'AWS integration';

export const commands = [
  {
    name: 'deploy',
    description: 'Deploy to AWS',
    handler: async (args, config) => { /* ... */ },
  },
];

export const hooks = {
  'before:deploy': async (config) => { /* ... */ },
  'after:deploy':  async (config) => { /* ... */ },
};
```

## State Management

```
~/.mycli/                       # User state directory (XDG-compliant)
├── config.yml                  # User configuration
├── cache/                      # Cached data (safe to delete)
│   ├── plugins.json
│   └── api-responses/
├── credentials.json            # Sensitive (chmod 600)
└── state.json                  # Session state
```

On Windows: `%APPDATA%\mycli\`
On macOS: `~/Library/Application Support/mycli/`
On Linux: `~/.config/mycli/` (XDG) or `$XDG_CONFIG_HOME/mycli/`

Always use the OS-correct path:

```javascript
import { env, homedir } from 'node:os';
import { join } from 'node:path';

function stateDir() {
  if (process.platform === 'win32') return join(env('APPDATA') ?? homedir(), 'mycli');
  if (process.platform === 'darwin') return join(homedir(), 'Library', 'Application Support', 'mycli');
  return join(env('XDG_CONFIG_HOME') ?? join(homedir(), '.config'), 'mycli');
}
```

```python
import os
from pathlib import Path

def state_dir() -> Path:
    if os.name == 'nt':
        return Path(os.environ.get('APPDATA', Path.home())) / 'mycli'
    if sys.platform == 'darwin':
        return Path.home() / 'Library' / 'Application Support' / 'mycli'
    return Path(os.environ.get('XDG_CONFIG_HOME', Path.home() / '.config')) / 'mycli'
```

```go
import "os"

func stateDir() string {
    switch runtime.GOOS {
    case "windows":
        return filepath.Join(os.Getenv("APPDATA"), "mycli")
    case "darwin":
        return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "mycli")
    default:
        if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
            return filepath.Join(x, "mycli")
        }
        return filepath.Join(os.Getenv("HOME"), ".config", "mycli")
    }
}
```

```rust
use directories::ProjectDirs;

let proj = ProjectDirs::from("com", "myorg", "mycli").unwrap();
let state_dir = proj.config_dir();
```

## Versioning & Updates

```javascript
// Check for updates in the background (non-blocking)
checkForUpdates().then(update => {
  if (update.available) {
    console.error(`Update available: ${update.version}`);
    console.error(`Run: npm install -g mycli@latest`);
  }
}).catch(() => { /* silently fail */ });
```

Always print update notice to **stderr**, not stdout — keeps stdout clean for piping.

## Performance Patterns

### Lazy loading

Only load dependencies for the command being run:

```javascript
// Don't:
import { heavy } from 'heavy-dep';  // loads on every invocation
program.command('rare').action(() => heavy());

// Do:
program.command('rare').action(async () => {
  const { heavy } = await import('heavy-dep');  // loads only when 'rare' runs
  heavy();
});
```

```python
# Don't:
import heavy_dep  # loads on every invocation

# Do:
def rare_command():
    import heavy_dep  # loads only when this function runs
    heavy_dep.do_thing()
```

### Caching

Cache expensive lookups to disk:

```javascript
import { Cache } from 'cache-manager';
import { join } from 'node:path';
import { stateDir } from './state.js';

const cache = new Cache({
  store: 'fs',
  cachePath: join(stateDir(), 'cache'),
  ttl: 3600,
});

const plugins = await cache.wrap('plugins', async () => fetchPlugins());
```

### Parallel operations

```javascript
const [config, plugins, updates] = await Promise.all([
  loadConfig(),
  loadPlugins(),
  checkForUpdates().catch(() => null),
]);
```

## Help Text Design

```
USAGE
  mycli <command> [options]

COMMANDS
  init [name]        Initialize a new project
  deploy <env>       Deploy to environment
  config <subcmd>    Manage configuration
  plugins <subcmd>   Manage plugins
  completions <shell> Generate shell completions

OPTIONS
  -h, --help         Show help
  -v, --version      Show version
  --config <file>    Config file path
  --verbose          Verbose output
  --no-color         Disable color output

EXAMPLES
  # Initialize a project
  mycli init my-app

  # Deploy to production
  mycli deploy production --dry-run

  # Generate bash completions
  mycli completions bash >> ~/.bashrc

Learn more: https://docs.mycli.dev
```

## Common Pitfalls

1. **Forgetting `--help` on subcommands** — users can't discover options
2. **Printing errors to stdout** — breaks piping
3. **Exit code 0 on error** — breaks shell scripting
4. **No `--version` flag** — users can't tell which version they're running
5. **Forgetting `NO_COLOR` / CI detection** — colors leak into logs
6. **Hardcoded `~/.mycli/` path** — breaks on Windows; use OS-correct path
7. **Heavy deps loaded eagerly** — startup > 1 second
8. **No SIGINT handler** — Ctrl+C leaves temp files / half-finished state
9. **Inconsistent flag names** — `--verbose` in one command, `--debug` in another
10. **No completions** — users must remember every subcommand
