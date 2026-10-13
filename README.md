# release-gate-crew

A four-agent team, built with [Docker Agent](https://docs.docker.com/ai/docker-agent/),
that reviews a service's container and CI configuration before release and
writes a PASS/BLOCK report. The whole team is one config file, written twice:
[`crew.yaml`](crew.yaml) and [`crew.hcl`](crew.hcl).

```
              root (lead)                think, todo
             /     |      \
 container_auditor  pipeline_auditor  scribe
 fs: ./target       fs: ./target      fs: ./reports
 shell: grep -n
```

- **root** delegates, never reads files, and makes the gate call.
- **container_auditor** reviews `target/Dockerfile` and `target/compose.yaml`.
- **pipeline_auditor** reviews the workflow in `target/ci/`.
- **scribe** writes `reports/gate-report.md` and can write nowhere else.

`target/` is a deliberately flawed sample service (unpinned base image, secret
in `ENV`, privileged container with the Docker socket mounted,
`pull_request_target` checking out PR code with `write-all`). Point the crew at
your own service by replacing `target/`.

## Guardrails in the config

| Layer | Where | What it does |
|---|---|---|
| Filesystem scope | `toolsets[].allow_list` | Auditors read only `./target`; scribe writes only `./reports`. |
| Hidden files | [`.agentsignore`](.agentsignore) | `.env`, keys, and certs are invisible to every agent. |
| Tool permissions | `permissions` | Read tools and `grep -n` auto-approve; network, deletion, pushes, and shell operators are denied. |
| Safety mode | `runtime.safety: restricted` | Anything not explicitly allowed is denied without a prompt, for unattended runs. |
| Spend | `budget` | One shared $0.50 / 10 minute ceiling across all four agents. |
| Isolation | `--sandbox` | Runs the crew in a Docker Sandboxes microVM. The real boundary. |

## Run it

Requires Go 1.24+ for the tests, and Docker Desktop 4.63+ (`docker agent`) or the standalone `docker-agent`
binary from [releases](https://github.com/docker/docker-agent/releases).

```bash
export ANTHROPIC_API_KEY=...
docker agent run crew.yaml                       # interactive TUI, try /gate
docker agent run crew.hcl --exec "Is target/ ready to ship?"
docker agent run crew.yaml --flavor cheap        # Haiku for the lead too
docker agent run --sandbox crew.yaml             # inside a Docker sandbox
```

Swap providers by editing the two `models` entries; nothing else changes.

## Project layout

```
crew.yaml, crew.hcl      the team (same config, two syntaxes)
prompts/                 one instruction file per agent
SPEC.md                  contract, guardrail requirements (R1-R9), evals (E1-E3)
internal/stub/           scripted OpenAI-compatible stub model (Go, stdlib only)
cmd/stubmodel/           binary that serves the stub on 127.0.0.1:8089
integration/             Go tests that drive the real docker-agent runtime
evals/                   docker agent eval sessions for real-model quality checks
target/                  deliberately flawed sample service
docs/FINDINGS.md         what the runs showed, including one footgun
```

## Test it

```bash
make lint          # gofmt + go vet
make test          # unit tests for the stub (race detector on)
make integration   # docker-agent + stub, both configs, SPEC R1-R9 and F1
make evals         # real models via docker agent eval, SPEC E1-E3 (needs keys + Docker)
```

The stub plays a fixed script per agent, so the integration suite can check
the wiring and guardrails without an API key: delegation happened,
`.agentsignore`, the allow-lists, and the deny rules refused what they should,
the report was written, and `target/` was not modified. Each check maps to a
requirement in [`SPEC.md`](SPEC.md). [CI](.github/workflows/ci.yml) runs
lint, unit, and integration tests on every push.

The stub proves the harness, not the reviews. The evals in `evals/` are the
quality check, and they need a real model.

The suite also reproduces a permissions footgun with prefix-style globs; see
[`docs/FINDINGS.md`](docs/FINDINGS.md).

## Share it

Agent configs are OCI artifacts. I have not yet checked whether `share push`
bundles the `prompts/` files referenced by `instruction_file`, so verify the
pulled agent before relying on it:

```bash
docker agent share push ./crew.yaml <your-hub-user>/release-gate-crew
docker agent run <your-hub-user>/release-gate-crew
```

Review any pulled config before running it: an author can declare
`runtime.safety: autonomous`. Your own `--safety` flag or user settings win.

## License

MIT
