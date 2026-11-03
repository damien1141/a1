package project

import "sync"

var (
	defaultProject *Project
	initOnce       sync.Once
)

// GetDefaultProject returns the singleton a1 workspace, discovered from the
// current working directory. It panics only if the global layout cannot be
// created (fs failure); configuration errors are surfaced by
// (*Project).LoadConfig so callers can report them without a stack trace.
//
// A project installed via SetDefaultProject wins over discovery: the Once
// body checks before it discovers, so consumers see the injected instance
// — with whatever config was loaded onto it — instead of a config-less
// re-discovery that silently discards it.
func GetDefaultProject() *Project {
	initOnce.Do(func() {
		if defaultProject != nil {
			return
		}
		p, err := Discover("")
		if err != nil {
			panic(err)
		}
		defaultProject = p
	})
	return defaultProject
}

// SetDefaultProject replaces the singleton project. Intended for tests
// only: the installed project is handed out by GetDefaultProject as-is
// until the next call, and SetDefaultProject(nil) restores lazy discovery
// from the live environment.
func SetDefaultProject(p *Project) {
	defaultProject = p
	initOnce = sync.Once{}
}
