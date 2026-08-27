# Repository Policies

## Documentation Language

- Write all original documentation prose, including documentation comments, in
  Standard English. Translation-specific documents may use their designated
  target language.
- Before completing documentation work, verify that all added or changed prose
  follows this rule.

## Git Operations

- Use Git only for read-only inspection, such as `git status`, `git diff`,
  `git log`, and `git show`.
- All local and remote Git mutations are human-only. Edit working-tree files
  only with non-Git tools, and hand any required Git mutation to a human.
- When asked to draft or review a commit message, read and follow the
  [Commit Message Guidelines](./CONTRIBUTING.md#commit-messages).

## Temporary Files

- Store all agent-controlled temporary files under `<repo-root>/.tmp/`, using a
  task-specific subdirectory.
- Configure tools to use that subdirectory; never use system temporary
  locations such as `/tmp`, `/var/tmp`, or the platform default.
- Remove the task-specific subdirectory before completion. Retain files only
  when needed for handoff or further diagnosis, and report their paths.
