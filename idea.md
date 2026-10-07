full recursive self-improvement (mutating its own go source or rewriting complex config files) is a 40-hour gold-plating project that will distract you from shipping. a folder of curated snippets (static memory / rag) is the deployable 80% solution and yields 90% of the utility. it scales your own heuristics without risking catastrophic self-deletion.

here is exactly how to build it in `a1` without it degrading into a useless junk drawer.

### the architecture (static playbooks)

reuse your existing `~/.a1/skills/` pattern. do not invent a new data format. 

**flow direction:** agent solves hard bug -> calls `memorize` tool -> writes markdown file to `~/.a1/playbooks/` -> next session injects the *index* of playbooks -> agent `read`s the specific playbook when triggered.

1. **the folder & format:** create `~/.a1/playbooks/`. use the exact same markdown + yaml frontmatter format as your skills. 
   ```yaml
   ---
   tags: [golang, concurrency, deadlock]
   trigger: "channel deadlock", "goroutine leak"
   ---
   # Fix: Channel Deadlock
   [code snippet and explanation here]
   ```
2. **the `memorize` tool:** add a new tool to your go binary. it takes a `title`, `tags`, and `content`. it writes the markdown file to disk and returns a success message. this closes the loop: the agent can now actively save things it learns.
3. **lazy injection (prevents context bloat):** do *not* dump the actual code snippets into the system prompt. dump an index. inject a list of `[filename: tags, trigger]` into the context. when the agent hits a problem that matches a trigger, it uses the existing `read` tool to fetch the full snippet. 

### where it breaks (and the fix)

*   **vector search hallucination:** if you rely purely on your existing `vector_search` tool to find snippets, it will retrieve tangentially related garbage when it's desperate. 
    *   *fix:* force exact string matching on the `trigger` keywords first. only fall back to `vector_search` if exact matches fail.
*   **stale snippets:** code evolves, but the saved snippets don't. 
    *   *fix:* add a `confidence` integer to the yaml frontmatter. every time the agent uses a snippet and it fails, it decrements the confidence. if it hits 0, the agent deletes the file.

### the 80% deployable loop

time estimate: about 2 hours to write the `memorize` tool and the index-injection logic in go; an afternoon if you also wire up the auto-pruning (confidence decay) logic.

1. scaffold `~/.a1/playbooks/`.
2. write the `memorize` tool (just standard go `os.WriteFile` with some string templating for the yaml).
3. update your session-startup logic to glob `~/.a1/playbooks/*.md`, parse the frontmatter, and inject the index into the system prompt.
4. tell the agent in its system doctrine: *"When you solve a non-trivial bug, call the `memorize` tool to save the fix as a playbook for future sessions."*
