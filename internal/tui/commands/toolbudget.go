package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/components/palette"
	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/tui/controller"
)

// ToolBudgetCommands owns the max-tool-calls surface: a Ctrl+K palette entry
// ("settings → max tool calls") with the common presets, a custom ladder for
// less common budgets, and a /maxtoolcalls <n> slash command for arbitrary
// values. The budget caps executed tool calls per turn in the agent engine;
// unrestricted gates (yolo mode, allow-all overrides) ignore it at
// enforcement time.
type ToolBudgetCommands struct {
	Ctrl *controller.EngineController
	Bus  *controller.Bus
}

// budgetPresets are the one-keystroke budgets offered in the main submenu.
var budgetPresets = []int{10, 25, 50, 100}

// budgetLadder is the finer "custom…" ladder — values people actually tune
// to between the presets without typing anything.
var budgetLadder = []int{5, 15, 20, 30, 40, 60, 75, 150, 200, 250, 500, 1000}

// Register wires the max-tool-calls entry into r (palette + slash).
func (t *ToolBudgetCommands) Register(r *CommandRegistry) {
	if t == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "maxtoolcalls",
		Description: "Max tool calls per turn (yolo/allow-all = unlimited)",
		Slash:       true,
		NeedsArgs:   true,
		Run: func(_ Context, args []string) error {
			t.runSlash(args)
			return nil
		},
		ArgCompleter: func(args []string) []ArgItem {
			if len(args) > 0 {
				return nil
			}
			items := []ArgItem{{Insert: "unlimited", Description: "no cap (yolo and allow-all modes always run unlimited)"}}
			for _, n := range append(append([]int{}, budgetPresets...), budgetLadder...) {
				items = append(items, ArgItem{Insert: strconv.Itoa(n), Description: fmt.Sprintf("%d tool calls per turn", n)})
			}
			return items
		},
		Build: func(ctx Context) palette.PaletteCommand {
			return t.buildPalette(ctx)
		},
	})
}

// buildPalette renders the settings → max tool calls submenu: presets with a
// ✓ on the current budget, plus a "custom…" row that pushes the finer ladder.
func (t *ToolBudgetCommands) buildPalette(ctx Context) palette.PaletteCommand {
	current := 0
	if t != nil && t.Ctrl != nil {
		current = t.Ctrl.MaxToolCalls()
	}
	sub := make([]palette.PaletteCommand, 0, len(budgetPresets)+2)

	unlimited := "  unlimited — no cap (yolo always runs unlimited)"
	if current <= 0 {
		unlimited = "✓ " + unlimited
	}
	sub = append(sub, palette.PaletteCommand{
		ID:       "toolbudget-unlimited",
		Verb:     unlimited,
		Keywords: []string{"unlimited", "infinite", "off", "none", "no", "cap"},
		Run: func() {
			t.set(0)
		},
	})
	for _, n := range budgetPresets {
		mark := "  "
		if n == current {
			mark = "✓ "
		}
		n := n
		sub = append(sub, palette.PaletteCommand{
			ID:       fmt.Sprintf("toolbudget-%d", n),
			Verb:     fmt.Sprintf("%s%d calls per turn", mark, n),
			Keywords: []string{strconv.Itoa(n), "budget", "limit", "cap", "calls"},
			Run: func() {
				t.set(n)
			},
		})
	}
	sub = append(sub, palette.PaletteCommand{
		ID:       "toolbudget-custom",
		Verb:     "custom…",
		Keywords: []string{"custom", "other", "number", "amount"},
		Run: func() {
			if ctx != nil {
				ctx.PushSubmenu("Custom tool-call budget", t.ladder(current))
			}
		},
	})

	return palette.PaletteCommand{
		ID:           "settings-toolbudget",
		Noun:         "settings",
		Verb:         "max tool calls",
		Keywords:     []string{"tool", "calls", "budget", "limit", "cap", "max", "yolo", "infinite", "unlimited"},
		SubmenuTitle: "Max Tool Calls (per turn)",
		Submenu:      sub,
	}
}

// ladder builds the fine-grained custom submenu, marking the current budget.
func (t *ToolBudgetCommands) ladder(current int) []palette.PaletteCommand {
	cmds := make([]palette.PaletteCommand, 0, len(budgetLadder))
	for _, n := range budgetLadder {
		mark := "  "
		if n == current {
			mark = "✓ "
		}
		n := n
		cmds = append(cmds, palette.PaletteCommand{
			ID:       fmt.Sprintf("toolbudget-custom-%d", n),
			Verb:     fmt.Sprintf("%s%d calls per turn", mark, n),
			Keywords: []string{strconv.Itoa(n), "calls"},
			Run: func() {
				t.set(n)
			},
		})
	}
	return cmds
}

// runSlash parses /maxtoolcalls args: bare shows the current budget, a number
// sets it, "unlimited"/"off"/"none" clears it.
func (t *ToolBudgetCommands) runSlash(args []string) {
	if t == nil || t.Ctrl == nil {
		return
	}
	if len(args) == 0 {
		current := t.Ctrl.MaxToolCalls()
		if current <= 0 {
			publishToast(t.Bus, "Max tool calls: unlimited (yolo always runs unlimited)", toast.ToastSuccess, 3*time.Second)
			return
		}
		publishToast(t.Bus, fmt.Sprintf("Max tool calls: %d per turn", current), toast.ToastSuccess, 3*time.Second)
		return
	}
	raw := strings.ToLower(strings.TrimSpace(strings.Join(args, "")))
	n := 0
	switch raw {
	case "unlimited", "inf", "infinite", "off", "none", "0":
		n = 0
	default:
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			publishToast(t.Bus, fmt.Sprintf("maxtoolcalls: expected a number or \"unlimited\", got %q", strings.Join(args, " ")), toast.ToastError, 3*time.Second)
			return
		}
		n = parsed
	}
	t.set(n)
}

// set applies the budget via the controller (engine rebind + config persist)
// and toasts the outcome. Errors surface as error toasts so the palette can
// close cleanly.
func (t *ToolBudgetCommands) set(n int) {
	if t == nil || t.Ctrl == nil {
		return
	}
	if err := t.Ctrl.SetMaxToolCalls(n); err != nil {
		publishToast(t.Bus, fmt.Sprintf("save max tool calls: %v", err), toast.ToastError, 3*time.Second)
		return
	}
	if n <= 0 {
		publishToast(t.Bus, "Max tool calls: unlimited (yolo always runs unlimited)", toast.ToastSuccess, 2*time.Second)
		return
	}
	publishToast(t.Bus, fmt.Sprintf("Max tool calls: %d per turn (yolo overrides with unlimited)", n), toast.ToastSuccess, 2*time.Second)
}
