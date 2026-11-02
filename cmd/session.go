package main

import (
	"fmt"
	"os"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
)

var (
	sessionsCommand = cli.Command{
		Name: "sessions",
		Desc: "list persisted sessions for this directory",
		Run:  func(_ []string, _ cli.Flags) error { return listSessions() },
	}

	sessionsListCommand = cli.Command{
		Name: "list",
		Desc: "list persisted sessions for this directory",
		Run:  func(_ []string, _ cli.Flags) error { return listSessions() },
	}
)

func init() {
	sessionsCommand.Add(&sessionsListCommand)
}

// listSessions prints persisted sessions for the current project, newest first.
func listSessions() error {
	proj := project.GetDefaultProject()
	dir := proj.SessionDir()
	list, err := session.ListSessions(dir)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(os.Stderr, "no sessions in %s\n", dir)
		return nil
	}
	for _, s := range list {
		fmt.Printf("%s  %s  %s\n", s.ID, s.Mtime.Format("2006-01-02 15:04:05"), s.Preview)
	}
	return nil
}
