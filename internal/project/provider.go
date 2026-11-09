package project

import "sync"

var (
	defaultProject *Project
	defaultErr     error
	initOnce       sync.Once
)

// discoverDefault is the Once body shared by both accessors: an installed
// project always wins over discovery, so the injected instance — with
// whatever config was loaded onto it — is handed out instead of a config-less
// re-discovery that silently discards it.
func discoverDefault() (*Project, error) {
	if defaultProject != nil {
		return defaultProject, nil
	}
	return Discover("")
}

// GetDefaultProject returns the singleton a1 workspace, discovered from the
// current working directory. It panics only if the global layout cannot be
// created (fs failure); configuration errors are surfaced by
// (*Project).LoadConfig so callers can report them without a stack trace.
//
// Boot surfaces (cmd/*, webserver, tui) rely on this fail-loud contract;
// request-time call sites that must not take down the process use
// TryDefaultProject instead.
func GetDefaultProject() *Project {
	initOnce.Do(func() {
		p, err := discoverDefault()
		if err != nil {
			panic(err)
		}
		defaultProject = p
	})
	return defaultProject
}

// TryDefaultProject is the non-panicking sibling of GetDefaultProject for
// call sites that run inside a request (tools) rather than at boot: the same
// lazy caching via the same Once, but a discovery failure comes back as an
// error instead of killing the process. It never returns both a project and
// an error.
func TryDefaultProject() (*Project, error) {
	initOnce.Do(func() {
		p, err := discoverDefault()
		if err != nil {
			defaultErr = err
			return
		}
		defaultProject = p
	})
	if defaultProject != nil {
		return defaultProject, nil
	}
	return nil, defaultErr
}

// SetDefaultProject replaces the singleton project. Intended for tests
// only: the installed project is handed out by GetDefaultProject as-is
// until the next call, and SetDefaultProject(nil) restores lazy discovery
// from the live environment.
func SetDefaultProject(p *Project) {
	defaultProject = p
	defaultErr = nil
	initOnce = sync.Once{}
}
