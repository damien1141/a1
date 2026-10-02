fix firefox browser
- DONE: the Firefox switch left the old `browser playwright.Browser` global
  declared but never assigned (LaunchPersistentContext returns a context,
  not a browser). browserClose called browser.Close() on a nil browser →
  nil-pointer panic on every /browser close. Removed the dead global; close
  now stops the Playwright driver and closes the persistent contexts.

add "default" thinking mode into the on the fly thinking mode switcher
- DONE: added a `default` ThinkMode sentinel (maps to Anthropic adaptive; other
  providers fall back to the preset's thinking config). Tab cycle and the
  settings → think palette now include it. Resolution lives in the controller
  so the engine/LLM client never see the sentinel.

wire default skills directory
- DONE: the directory wiring was already correct (Discover installs the
  embedded skill library into ~/.a1/skills; loadConfig defaults skill_path to
  that dir). The bug was that every installed skill failed to parse:
  parseFrontmatter rejected indented frontmatter lines, and every shipped
  skill carries a nested metadata: map block, so LoadSkills returned empty and
  the skills palette showed "No skills found". Indented continuation lines are
  now skipped. Added TestParse_LoadsEmbeddedDefaults and
  TestSkillPathDefaultsToGlobalSkillsDir as regression guards.

fix auto-compaction on context overflow
- DONE: runCompact(force=true) skipped both triggers by design, but the
  no-op guard also applied to the force path, so the overflow-recovery branch
  never compacted and burned its single retry on a second overflow. The guard
  now requires !force. Added TestRunCompact_ForceCompactsWhenHistoryExists.