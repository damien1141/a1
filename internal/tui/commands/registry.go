package commands

import (
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/components/mention"
	"github.com/damien1141/a1/internal/components/palette"
)

// ArgItem is one suggestion in the arg completer.
type ArgItem struct {
	Insert      string // text to insert into the composer
	Description string // optional hint
}

// Command is one registered slash and/or palette entry.
type Command struct {
	Name        string
	Description string
	Slash       bool
	// Insert is written into the composer on slash-picker accept.
	// Empty defaults to "/"+Name (or "/"+Name+" " when NeedsArgs).
	// A trailing space means "fill composer, do not auto-submit".
	Insert string
	// NeedsArgs means bare "/name" (picker accept or submit) should leave
	// Insert in the composer so the user can type arguments. Optional-arg
	// commands use a trailing Insert space without NeedsArgs.
	NeedsArgs bool

	// Run handles slash dispatch. Nil for palette-only commands.
	Run func(ctx Context, args []string) error

	// Build builds a Ctrl+K palette entry. Nil for slash-only commands.
	Build func(ctx Context) palette.PaletteCommand

	// ArgCompleter returns suggestions for the next arg token given the
	// already-typed args. Empty slice means no arg completion available.
	ArgCompleter func(args []string) []ArgItem

	fromExt bool // dropped on extensions reload; cannot replace builtins
}

// CommandRegistry is the single catalog for composer `/` and Ctrl+K palette.
type CommandRegistry struct {
	mu   sync.RWMutex
	cmds []Command
	by   map[string]int // lower(name) → index in cmds
}

// NewCommandRegistry returns an empty registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{by: make(map[string]int)}
}

func finalizeSlashInsert(cmd *Command) {
	if !cmd.Slash {
		return
	}
	if cmd.Insert == "" {
		cmd.Insert = "/" + cmd.Name
		if cmd.NeedsArgs {
			cmd.Insert += " "
		}
	} else if cmd.NeedsArgs && !strings.HasSuffix(cmd.Insert, " ") {
		cmd.Insert += " "
	}
}

// Register adds cmd. Duplicate names (case-insensitive) replace the prior entry.
func (r *CommandRegistry) Register(cmd Command) {
	name := strings.ToLower(strings.TrimSpace(cmd.Name))
	if name == "" {
		return
	}
	cmd.Name = strings.TrimSpace(cmd.Name)
	finalizeSlashInsert(&cmd)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = make(map[string]int)
	}
	if i, ok := r.by[name]; ok {
		r.cmds[i] = cmd
		return
	}
	r.by[name] = len(r.cmds)
	r.cmds = append(r.cmds, cmd)
}

// registerExt adds a slash command from an extension.
// Returns false if name is empty or already taken by a builtin.
func (r *CommandRegistry) registerExt(cmd Command) bool {
	name := strings.ToLower(strings.TrimSpace(cmd.Name))
	if name == "" {
		return false
	}
	cmd.Name = strings.TrimSpace(cmd.Name)
	cmd.fromExt = true
	finalizeSlashInsert(&cmd)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = make(map[string]int)
	}
	if i, ok := r.by[name]; ok {
		if !r.cmds[i].fromExt {
			return false
		}
		r.cmds[i] = cmd
		return true
	}
	r.by[name] = len(r.cmds)
	r.cmds = append(r.cmds, cmd)
	return true
}

// clearExtCommands removes every command registered via registerExt.
func (r *CommandRegistry) clearExtCommands() {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := make([]Command, 0, len(r.cmds))
	r.by = make(map[string]int, len(r.cmds))
	for _, c := range r.cmds {
		if c.fromExt {
			continue
		}
		r.by[strings.ToLower(c.Name)] = len(kept)
		kept = append(kept, c)
	}
	r.cmds = kept
}

// DispatchSlash runs a `/name …` line. Returns false if not a known slash command.
func (r *CommandRegistry) DispatchSlash(text string, ctx Context) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	name := strings.TrimPrefix(fields[0], "/")
	cmd, ok := r.lookup(name)
	if !ok || !cmd.Slash || cmd.Run == nil {
		return false
	}
	_ = cmd.Run(ctx, fields[1:])
	return true
}

// FilterSlash returns mention items for the slash picker (name prefix match).
func (r *CommandRegistry) FilterSlash(query string) []mention.Item {
	q := strings.ToLower(strings.TrimSpace(query))
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]mention.Item, 0, len(r.cmds))
	for _, c := range r.cmds {
		if !c.Slash {
			continue
		}
		if q != "" && !strings.HasPrefix(strings.ToLower(c.Name), q) {
			continue
		}
		out = append(out, mention.Item{
			Path:        c.Name,
			Description: c.Description,
		})
	}
	return out
}

// LookupInsert returns the Insert string for a slash command name, or empty.
func (r *CommandRegistry) LookupInsert(name string) string {
	cmd, ok := r.lookup(name)
	if !ok || !cmd.Slash {
		return ""
	}
	return cmd.Insert
}

// Lookup returns the Command for a slash command name.
func (r *CommandRegistry) Lookup(name string) (Command, bool) {
	return r.lookup(name)
}

// FilterSlashArgs returns arg suggestions for a slash command after the command
// token. The query is the current unfinished arg token (may be empty). Returned
// items use Insert as the full token to insert into the composer.
func (r *CommandRegistry) FilterSlashArgs(name, query string, args []string) []mention.Item {
	q := strings.ToLower(strings.TrimSpace(query))
	r.mu.RLock()
	cmd, ok := r.lookup(name)
	r.mu.RUnlock()
	if !ok || cmd.ArgCompleter == nil {
		return nil
	}
	items := cmd.ArgCompleter(args)
	if len(items) == 0 {
		return nil
	}
	out := make([]mention.Item, 0, len(items))
	for _, it := range items {
		if q != "" && !strings.HasPrefix(strings.ToLower(it.Insert), q) {
			continue
		}
		out = append(out, mention.Item{Path: it.Insert, Description: it.Description})
	}
	return out
}

// IncompleteSlash reports whether text is a known NeedsArgs slash with no
// arguments. insert is the composer value to leave for the user to finish.
func (r *CommandRegistry) IncompleteSlash(text string) (insert string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) != 1 {
		return "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if name == "" || name == fields[0] {
		return "", false
	}
	cmd, found := r.lookup(name)
	if !found || !cmd.Slash || !cmd.NeedsArgs {
		return "", false
	}
	return cmd.Insert, true
}

// BuildPalette returns Ctrl+K root commands in registration order.
func (r *CommandRegistry) BuildPalette(ctx Context) []palette.PaletteCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]palette.PaletteCommand, 0, len(r.cmds))
	for _, c := range r.cmds {
		if c.Build == nil {
			continue
		}
		out = append(out, c.Build(ctx))
	}
	return out
}

func (r *CommandRegistry) lookup(name string) (Command, bool) {
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.by[key]
	if !ok {
		return Command{}, false
	}
	return r.cmds[i], true
}

// SlashCommands returns slash catalog entries (for tests / introspection).
func (r *CommandRegistry) SlashCommands() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Command, 0, len(r.cmds))
	for _, c := range r.cmds {
		if c.Slash {
			out = append(out, c)
		}
	}
	return out
}
