package commands

// CodeCommands owns the /code slash command, which opens a file in the
// full-screen viewer.
type CodeCommands struct {
	Open func(args []string)
}

// Register wires /code into r.
func (c *CodeCommands) Register(r *CommandRegistry) {
	if c == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "code",
		Description: "Open a file in the viewer — /code path[:line]",
		Slash:       true,
		NeedsArgs:   true,
		Insert:      "/code ",
		Run: func(_ Context, args []string) error {
			if c.Open != nil {
				c.Open(args)
			}
			return nil
		},
	})
}
