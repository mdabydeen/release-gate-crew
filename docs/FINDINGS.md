# What the offline runs showed

Observed with docker-agent v1.150.0 on Linux (amd64), using the scripted stub
model in `internal/stub/`. Every item below is a subtest in
`integration/crew_test.go` (requirement IDs in `SPEC.md`), so CI re-confirms
it on each push, except "Isolated context", which was read from the requests
the runtime sent the stub and is pinned by the unit test
`TestScribeReadsDelegatedTask`. None of it says anything about review quality;
the stub is not a reviewer.

## Held as configured

| Control | Config | Observed |
|---|---|---|
| Delegation | `sub_agents` on `root` | Lead called `transfer_task` for each of the three sub-agents in turn. |
| Isolated context | (default) | A sub-agent receives only its task, inside a `<task>` block in its system prompt; its user turn is `Please proceed.` Nothing else from the lead's conversation is passed. |
| Secret hiding | `.agentsignore` lists `.env` | `read_file target/.env` returned `path "target/.env" is excluded by .agentsignore`. |
| Read scope | `filesystem.allow_list: ["./target"]` | `read_file prompts/lead.md` from `pipeline_auditor` returned `outside the allowed directories (./target)`. |
| Write scope | `write_file:path=*reports/*` allowed, `runtime.safety: restricted` | `scribe` wrote `reports/gate-report.md`; its write to `target/Dockerfile` was denied by the restricted safety mode without a prompt. |
| Compound shell | deny `*&&*`, `*;*`, `*\|*`, backticks, `$(` | `grep -n FROM target/Dockerfile && curl --version` was denied. |

## The footgun: prefix globs and compound commands

Permission patterns are globs matched against the whole command string, and
`*` matches anything, spaces included. Prefix-style rules therefore do not
compose with shell operators:

```yaml
permissions:
  allow: ["shell:cmd=grep -n *"]
  deny:  ["shell:cmd=curl *"]
```

`grep -n FROM target/Dockerfile && curl --version` matches the allow pattern
(it starts with `grep -n `) and does not match the deny pattern (it does not
start with `curl `). Deny is checked first, finds nothing, then allow approves
it. With `runtime.safety: restricted` it still ran, because an explicit allow
rule outranks the safety mode. Reproduce with:

```bash
go run ./cmd/stubmodel &
STUB_API_KEY=stub docker agent run crew.yaml \
  --flavor offline --flavor naive-permissions --exec "go"
# the shell result contains curl's version banner
```

This is consistent with the documentation, which says permissions are enforced
client-side and are not a security boundary, and points to sandbox mode for
isolation. It is still easy to miss, because prefix rules like `cat *` and
`rm *` are the common pattern in examples. Mitigations used in `crew.yaml`:

1. Deny shell operators outright (`*&&*`, `*;*`, `*|*`, backticks, `$(`).
2. Write deny rules as `*curl*` rather than `curl *`.
3. Keep the allow list to one narrow command.
4. Run untrusted work with `--sandbox`, which is the actual boundary.

## Not tested here

- Runs against a real model provider. Use your own key; see the README.
- `--sandbox` (needs Docker Desktop with Docker Sandboxes, or `sbx`).
- `docker agent share push` to a registry.
