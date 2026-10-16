// Package project provides the a1 workspace layout and configuration.
//
// Discover creates the global a1 home (~/.a1) with its standard
// subdirectories (bin, skills, hooks, session, jobs) so downloaded tool
// binaries, SKILL.md files, hook manifests, and persisted sessions have a
// known home. Startup ensures the layout exists, then tools such as fd/ripgrep
// are downloaded into the bin directory when missing.
package project
