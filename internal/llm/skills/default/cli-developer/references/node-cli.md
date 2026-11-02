# Node.js CLI Development

## Frameworks

| Framework | Best for | Notes |
|---|---|---|
| **commander** | Most CLIs | Simple, popular, TS support |
| **yargs** | Complex arg parsing + middleware | Powerful but heavier |
| **oclif** | Large plugin-based CLIs (Salesforce, Heroku, Sfdx) | Plugin architecture built-in |
| **clipanion** | Type-safe + class-based (used by Yarn) | Less mainstream |

## Commander (recommended)

```javascript
#!/usr/bin/env node
import { Command } from 'commander';
import { readFileSync } from 'node:fs';

const pkg = JSON.parse(readFileSync(new URL('./package.json', import.meta.url)));

const program = new Command();
program
  .name('mycli')
  .description('My awesome CLI')
  .version(pkg.version);

// Simple command
program
  .command('init [name]')
  .description('Initialize a new project')
  .option('-t, --template <type>', 'Project template', 'default')
  .option('-f, --force', 'Overwrite existing')
  .action(async (name = 'my-app', opts) => {
    await initProject(name, opts);
  });

// Subcommand with positional arg
program
  .command('deploy <environment>')
  .description('Deploy to environment')
  .option('-d, --dry-run', 'Preview without executing')
  .option('--config <file>', 'Config file path')
  .action(async (environment, opts) => {
    if (opts.dryRun) {
      console.error(`Would deploy to ${environment}`);
      return;
    }
    await deploy(environment, opts);
  });

// Nested subcommands
const config = program.command('config').description('Manage configuration');
config.command('get <key>').action((key) => console.log(getConfig(key)));
config.command('set <key> <value>').action((key, value) => setConfig(key, value));
config.command('list').action(() => listConfig());

// Hook (runs before all commands)
program.hook('preAction', (thisCommand, actionCommand) => {
  console.error(`Running ${actionCommand.name()}...`);
});

await program.parseAsync(process.argv);
```

## Yargs (advanced arg parsing)

```javascript
#!/usr/bin/env node
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';

yargs(hideBin(process.argv))
  .scriptName('mycli')
  .command(
    'deploy <env>',
    'Deploy to environment',
    (yargs) => yargs
      .positional('env', {
        describe: 'Environment',
        choices: ['dev', 'staging', 'prod'],
        demandOption: true,
      })
      .option('force', { alias: 'f', type: 'boolean', description: 'Force deploy' })
      .option('dry-run', { type: 'boolean', description: 'Preview only' }),
    async (argv) => {
      await deploy(argv.env, { force: argv.force, dryRun: argv.dryRun });
    }
  )
  .command('config', 'Manage configuration', (yargs) =>
    yargs
      .command('get <key>', 'Get value', {}, (argv) => console.log(getConfig(argv.key)))
      .command('set <key> <value>', 'Set value', {}, (argv) => setConfig(argv.key, argv.value))
      .command('list', 'List all', {}, () => listConfig())
      .demandCommand()
  )
  .middleware([(argv) => {
    // Runs before every command
    if (!configValid()) throw new Error('Config invalid — run `mycli init`');
  }])
  .demandCommand()
  .recommendCommands()       // suggest on typo
  .completion('completion')  // built-in completion
  .help()
  .alias('help', 'h')
  .version()
  .alias('version', 'v')
  .parse();
```

## oclif (plugin architecture)

For large, plugin-based CLIs. Used by Salesforce CLI, Heroku CLI, Sfdx.

```bash
npx oclif generate mycli
cd mycli
npx oclif command deploy
```

Generated structure:

```
mycli/
├── src/
│   ├── commands/
│   │   ├── deploy.ts          # one class per command
│   │   └── config/
│   │       ├── get.ts
│   │       └── set.ts
│   ├── hooks/
│   │   └── init.ts
│   └── index.ts
├── package.json
└── oclif.config.js
```

```typescript
// src/commands/deploy.ts
import { Command, Flags } from '@oclif/core';

export default class Deploy extends Command {
  static description = 'Deploy to environment';
  static args = [{ name: 'environment', required: true }];

  static flags = {
    'dry-run': Flags.boolean({ char: 'd', description: 'Preview only' }),
    force: Flags.boolean({ char: 'f', description: 'Skip prompts' }),
  };

  async run(): Promise<void> {
    const { args, flags } = await this.parse(Deploy);
    if (flags['dry-run']) {
      this.log(`Would deploy to ${args.environment}`);
      return;
    }
    await deploy(args.environment, flags.force);
  }
}
```

## Interactive Prompts (inquirer / @inquirer/prompts)

```javascript
import { select, input, confirm, password, checkbox } from '@inquirer/prompts';

// Text input
const name = await input({ message: 'Project name:', default: 'my-project' });

// Select (single choice)
const env = await select({
  message: 'Select environment:',
  choices: [
    { value: 'dev',  name: 'Development' },
    { value: 'staging', name: 'Staging' },
    { value: 'prod', name: 'Production' },
  ],
});

// Checkbox (multi)
const features = await checkbox({
  message: 'Select features:',
  choices: [
    { value: 'typescript', name: 'TypeScript', checked: true },
    { value: 'eslint',     name: 'ESLint',     checked: true },
    { value: 'prettier',   name: 'Prettier' },
  ],
});

// Confirm
const ok = await confirm({ message: 'Deploy to production?', default: false });

// Password (masked)
const pw = await password({ message: 'Enter password:', mask: '*' });
```

## Terminal Output (chalk + ora)

```javascript
import chalk from 'chalk';
import ora from 'ora';

// Auto-detects TTY + honors NO_COLOR / CI
console.log(chalk.blue('ℹ'), 'Info message');
console.log(chalk.green('✔'), 'Success');
console.log(chalk.yellow('⚠'), 'Warning');
console.log(chalk.red('✖'), 'Error');

// Spinner (only renders in TTY)
const spinner = ora('Installing dependencies...').start();
try {
  await installDeps();
  spinner.succeed('Dependencies installed');
} catch (err) {
  spinner.fail('Installation failed');
  console.error(chalk.red(err.message));
  process.exit(1);
}

// Multiple parallel spinners
const tasks = {
  api: ora('Deploying API...').start(),
  web: ora('Deploying web...').start(),
};
await Promise.all([
  deployApi().then(() => tasks.api.succeed('API deployed')),
  deployWeb().then(() => tasks.web.succeed('Web deployed')),
]);
```

## Progress Bars (cli-progress)

```javascript
import cliProgress from 'cli-progress';

const bar = new cliProgress.SingleBar({
  format: ' {bar} | {percentage}% | {value}/{total}',
  hideCursor: true,
  clearOnComplete: false,
}, cliProgress.Presets.shades_classic);

bar.start(100, 0);
for (let i = 0; i <= 100; i++) {
  await processItem(i);
  bar.update(i);
}
bar.stop();

// Multi-bar
const multibar = new cliProgress.MultiBar({ hideCursor: true });
const b1 = multibar.create(100, 0, { task: 'API' });
const b2 = multibar.create(100, 0, { task: 'Web' });
await Promise.all([processApi(b1), processWeb(b2)]);
multibar.stop();
```

## File System Helpers

```javascript
import fs from 'fs-extra';
import { globby } from 'globby';

// Read JSON
const config = await fs.readJson('config.json');
await fs.writeJson('out.json', data, { spaces: 2 });

// Ensure dir exists
await fs.ensureDir('dist/assets');

// Copy with filter
await fs.copy('templates/app', targetDir, {
  filter: (src) => !src.includes('node_modules'),
});

// Glob find files
const files = await globby(['src/**/*.ts', '!src/**/*.test.ts']);
```

## Error Handling + SIGINT

```javascript
process.on('SIGINT', () => {
  console.error('\nOperation cancelled');
  process.exit(130);
});

process.on('unhandledRejection', (err) => {
  console.error(chalk.red('✖ Fatal:'), err.message);
  if (process.env.DEBUG) console.error(err.stack);
  process.exit(1);
});

// In command actions:
program
  .command('deploy')
  .action(async () => {
    try {
      await deploy();
    } catch (err) {
      if (err.code === 'EACCES') {
        console.error(chalk.red('Permission denied'));
        console.error('Try: sudo mycli deploy  OR  check file permissions');
        process.exit(77);   // permission denied (custom)
      } else if (err.code === 'ENOENT') {
        console.error(chalk.red('File not found:'), err.path);
        process.exit(127);
      } else {
        console.error(chalk.red('Deploy failed:'), err.message);
        if (process.env.DEBUG) console.error(err.stack);
        process.exit(1);
      }
    }
  });
```

## package.json for Distribution

```json
{
  "name": "mycli",
  "version": "1.0.0",
  "description": "My awesome CLI",
  "type": "module",
  "bin": {
    "mycli": "./bin/cli.js"
  },
  "files": [
    "bin/",
    "lib/",
    "templates/"
  ],
  "engines": {
    "node": ">=18.0.0"
  },
  "dependencies": {
    "commander": "^12.0.0",
    "@inquirer/prompts": "^5.0.0",
    "chalk": "^5.3.0",
    "ora": "^8.0.0",
    "fs-extra": "^11.0.0",
    "globby": "^14.0.0"
  },
  "devDependencies": {
    "vitest": "^1.0.0",
    "execa": "^9.0.0"
  },
  "scripts": {
    "test": "vitest"
  }
}
```

`bin/cli.js`:
```javascript
#!/usr/bin/env node
import '../lib/index.js';
```

Make executable: `chmod +x bin/cli.js`

## Testing CLIs

```javascript
// tests/cli.test.js
import { execaCommand } from 'execa';
import { describe, it, expect } from 'vitest';

describe('mycli', () => {
  it('--version shows semver', async () => {
    const { stdout } = await execaCommand('node bin/cli.js --version');
    expect(stdout).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it('--help shows USAGE', async () => {
    const { stdout } = await execaCommand('node bin/cli.js --help');
    expect(stdout).toContain('USAGE');
    expect(stdout).toContain('COMMANDS');
  });

  it('deploy --dry-run prints preview', async () => {
    const { stderr } = await execaCommand('node bin/cli.js deploy prod --dry-run');
    expect(stderr).toContain('Would deploy to prod');
  });

  it('unknown command exits with code 2', async () => {
    await expect(execaCommand('node bin/cli.js invalid-cmd')).rejects.toMatchObject({
      exitCode: 2,
    });
  });
});
```

## Distribution

```bash
# Local install (dev)
npm install -g .

# Publish to npm
npm version patch
npm publish

# Install via npx (no install)
npx mycli deploy prod
```

## Startup Time Optimization

Node CLIs often hit 200-500ms startup due to module loading. Tips:

1. **Lazy import heavy deps** — only load when needed:
   ```javascript
   program.command('rare').action(async () => {
     const { heavy } = await import('heavy-dep');
     heavy();
   });
   ```
2. **Use ESM** — faster cold start than CommonJS in Node 18+
3. **Avoid top-level `await`** for non-critical work
4. **Bundle** with esbuild/rollup to reduce module resolution
5. **Strip unused deps** with `depcheck`
6. **Benchmark** with `hyperfine`:
   ```bash
   hyperfine --warmup 3 'node bin/cli.js --version'
   ```

## Shell Completions (commander auto)

```javascript
// In your CLI:
program.command('completions <shell>')
  .description('Generate shell completions')
  .action((shell) => {
    // commander doesn't auto-generate; use 'commander-completions' plugin or roll your own
    // OR use oclif which has built-in
  });
```

For Node CLIs, **oclif** has the best built-in completion support:

```bash
mycli completions >> ~/.bashrc
mycli completions --shell zsh >> ~/.zshrc
```

## Common Pitfalls (Node-specific)

1. **Top-level `await` in CommonJS** — not supported. Use ESM (`"type": "module"`) or wrap in async IIFE.
2. **`fs/promises` vs `fs`** — mix sync/async carefully; prefer async for CLI tools.
3. **`process.exit()` doesn't flush stdout** — use `process.exitCode = 1` instead, let Node flush.
4. **Shebang missing** — must be `#!/usr/bin/env node` and file is `chmod +x`.
5. **No `engines` field** — users on old Node get cryptic errors.
6. **Heavy deps in `dependencies`** — slows startup. Move optional deps to `optionalDependencies`.
7. **`console.log` for errors** — pollutes stdout. Use `console.error` or `process.stderr.write`.
8. **Uncaught promise rejections** — Node 15+ exits with non-zero by default, but error message is cryptic. Always wrap with try/catch.
