package commands

// DiffCommands owns the /diff slash command.
type DiffCommands struct {
	Open func(args []string)
}

// Register wires /diff into r.
func (d *DiffCommands) Register(r *CommandRegistry) {
	if d == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "diff",
		Description: "Review git diff — /diff, /diff staged, /diff HEAD",
		Slash:       true,
		Insert:      "/diff ",
		Run: func(_ Context, args []string) error {
			if d.Open != nil {
				d.Open(args)
			}
			return nil
		},
		ArgCompleter: func(args []string) []ArgItem {
			if len(args) == 0 {
				return []ArgItem{
					{Insert: "staged", Description: "staged changes"},
					{Insert: "HEAD", Description: "compare against HEAD"},
				}
			}
			return nil
		},
	})
}
