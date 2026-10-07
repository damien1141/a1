package commands

import (
	"encoding/json"
	"fmt"

	"github.com/damien1141/a1/internal/tools/snapshottool"
)

// SnapshotCommands owns the /snapshot slash command.
type SnapshotCommands struct{}

// Register wires /snapshot into r.
func (s *SnapshotCommands) Register(r *CommandRegistry) {
	if s == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "snapshot",
		Description: "Explicit git snapshot tool — /snapshot create|rollback|list|delete [branch]",
		Slash:       true,
		NeedsArgs:   true,
		Insert:      "/snapshot ",
		Run: func(_ Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: /snapshot create|rollback|list|delete [branch]")
			}
			action := args[0]
			var in struct {
				Action string `json:"action"`
				Branch string `json:"branch,omitempty"`
			}
			in.Action = action
			if len(args) > 1 {
				in.Branch = args[1]
			}
			raw, _ := json.Marshal(in)
			_, err := snapshottool.SnapshotTool().Run(nil, raw)
			return err
		},
		ArgCompleter: func(args []string) []ArgItem {
			if len(args) == 0 {
				return []ArgItem{
					{Insert: "create", Description: "create snapshot"},
					{Insert: "rollback", Description: "rollback snapshot"},
					{Insert: "list", Description: "list snapshots"},
					{Insert: "delete", Description: "delete snapshot"},
				}
			}
			return nil
		},
	})
}
