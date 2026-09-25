package commands

import (
	"github.com/damien1141/a1/internal/components/palette"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
	"github.com/damien1141/a1/internal/tui/footer"
	"github.com/damien1141/a1/internal/tui/transcript"
	"github.com/damien1141/a1/internal/util/gitx"
)

// BuiltinComposer is the composer surface builtin domains need.
// *composer.ComposerPane satisfies this without importing composer here.
type BuiltinComposer interface {
	SetModelLabel(name, thinkLevel string)
	AddPendingSkill(name string)
	SetPaletteCommands([]palette.PaletteCommand)
}

// Builtin is the assembled command surface owned by the TUI shell.
type Builtin struct {
	Registry *CommandRegistry
	Sessions *SessionCommands
	Branches *BranchCommands
	Ext      *ExtCommands
}

// NewBuiltinRegistry constructs the registry and every builtin domain handler.
// Call Bind after Submitter / command Context exist.
func NewBuiltinRegistry(
	bus *controller.Bus,
	ctrl *controller.EngineController,
	composer BuiltinComposer,
	tr *transcript.TranscriptPane,
	ft *footer.FooterChrome,
	modelNames []string,
	skillPath string,
	openDiff func(args []string),
	openCode func(args []string),
) *Builtin {
	r := NewCommandRegistry()
	ext := &ExtCommands{
		Registry: r,
		Ctrl:     ctrl,
		Composer: composer,
		Footer:   ft,
		Bus:      bus,
	}
	sessions := NewSessionCommands(ctrl, tr, ft, bus, ext.Sync)
	branches := NewBranchCommands(bus)
	settings := &SettingsCommands{
		Ctrl:       ctrl,
		Bus:        bus,
		Composer:   composer,
		ModelNames: append([]string(nil), modelNames...),
	}
	skills := &SkillsCommands{
		SkillPath: skillPath,
		Add:       composer,
	}
	diff := &DiffCommands{Open: openDiff}
	code := &CodeCommands{Open: openCode}

	sessions.Register(r)
	branches.Register(r)
	settings.Register(r)
	ext.Register(r)
	skills.Register(r)
	diff.Register(r)
	code.Register(r)

	return &Builtin{Registry: r, Sessions: sessions, Branches: branches, Ext: ext}
}

// Bind attaches late UI collaborators (Submitter, pickers, stream guard).
func (b *Builtin) Bind(
	submitter extSubmitter,
	commandCtx func() Context,
	openPicker func(items []session.SessionMeta, currentID string),
	openBranchPicker func(branches []gitx.Branch, recent []string, onAccept func(name string)),
	cwd func() string,
	streamActive func() bool,
) {
	if b == nil {
		return
	}
	if b.Ext != nil {
		b.Ext.Submitter = submitter
		b.Ext.CommandCtx = commandCtx
	}
	if b.Sessions != nil {
		b.Sessions.OpenPicker = openPicker
		b.Sessions.StreamActive = streamActive
	}
	if b.Branches != nil {
		b.Branches.OpenOverlay = openBranchPicker
		b.Branches.Dir = cwd
		b.Branches.StreamActive = streamActive
	}
}
