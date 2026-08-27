# Contributing Guidelines

## Commit Messages

New commit messages must follow
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/):

```text
<type>[optional scope][!]: <description>

[optional body]

[optional footer(s)]
```

- Use one of these types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`,
  `build`, `ci`, `chore`, `style`, or `revert`.
- Use a scope only when it names a stable, recognizable part of the project.
- Write the description in Standard English as a concise imperative phrase,
  without a trailing period.
- Use a body when the motivation, behavior change, or important trade-off is not
  clear from the description alone.
- Mark a breaking change with `!` before the colon or a `BREAKING CHANGE:`
  footer, and explain the impact in the body or footer.
- Keep each commit focused on one coherent change.

### AI Assistance

Reserve authorship and human-attestation trailers, including
`Co-authored-by`, `Signed-off-by`, `Reviewed-by`, and `Tested-by`, for human
contributors.

When an AI coding agent materially contributes to a commit, add one trailer
for each materially involved agent:

```
Assisted-by: <agent>:<model>[:<reasoning-effort>]
```

- Write the agent identifier in lowercase kebab case, such as `codex` or
  `github-copilot`.
- Use the exact model identifier reported by the agent.
- Include the optional reasoning effort only when the agent reports the
  effective setting. Use one of `none`, `low`, `medium`, `high`, `xhigh`, or
  `max`; do not infer an unavailable setting.
- Treat assistance as material when it shapes the implementation, design,
  tests, documentation, or commit message. The trailer is not required for
  trivial completion, mechanical formatting or renaming, or spelling and
  grammar correction.
- Review and understand the entire change, verify its provenance and
  licensing, run appropriate tests, and remain responsible for the
  contribution.

For example, a commit materially assisted by Codex using GPT-5.6 Sol with max
reasoning effort uses:

```
Assisted-by: codex:gpt-5.6-sol:max
```
