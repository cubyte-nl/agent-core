# agent-core

The shared Go core for Cubyte's scope-gated, LLM-driven agents. It points an
external LLM at a recorded boundary and runs the plan/act/report loop behind a
single hard rule: the model only ever acts through tools, and every tool call
is checked against the boundary before it runs.

agent-core carries the agent-independent machinery — the loop, the tool
plumbing, the access-gate primitives, the LLM client, the knowledge retriever,
and the conformance checks. Each agent (MOLE for offensive recon, Collie, …)
keeps its own typed boundary, tools, prompt, and report, and wires them into
this core.

```go
import "github.com/cubyte-nl/agent-core/agent"
```

Module `github.com/cubyte-nl/agent-core`, Go 1.27+, no external dependencies.

## Packages

| Package | Role |
|---|---|
| `agent` | The LLM-driven loop, generic over the agent's boundary type `B`. Owns only the control flow — chat, tool dispatch, the operator-confirmation step, the step and time budgets with a forced final report, the transcript, retries, and truncated-response handling. Everything domain-specific (prompt text, action-log rendering, report body, transcript metadata) is injected through `Config`. Entry point: `agent.Run`. |
| `tools` | The capability plumbing: the `Boundary` interface, the structured `Result`, the `Tool[B]` interface, and the `Registry[B]` that exposes tools to the model. Generic over `B`, so each agent keeps a fully-typed boundary while sharing the plumbing. |
| `scope` | The access-gate primitives: parse a target down to a bare host, match names and addresses against scope patterns, resolve a host, and the fail-closed `Check` decision. Each agent keeps its own typed `Scope`; the agent-independent logic lives here. |
| `llm` | A minimal client for OpenAI-compatible chat-completions endpoints with function calling. Endpoint, key and model come from the environment under a per-agent prefix: `<PREFIX>_LLM_BASE_URL`, `<PREFIX>_LLM_API_KEY`, `<PREFIX>_LLM_MODEL`. |
| `knowledge` | The mini-RAG: index markdown docs (chunked by heading) and return the sections most relevant to a query, so the model consults them at the moment it needs them instead of carrying the full text in the prompt. Retrieval is deterministic and dependency-free (weighted keyword overlap), isolated behind `Search` so an embedding scorer can replace it later. |
| `coretest` | The conformance checks every agent built on the core is expected to satisfy. An agent's own test wires its concrete boundary and tools into these helpers, so the shared safety contracts — fail-closed boundary, read-only runs never exposing a mutating tool — are re-verified per agent rather than trusted by convention. |

## The boundary

The boundary is the hard limit inside which an agent may operate. Every tool
asks it before touching a target; out-of-scope targets are refused, and the
gate fails closed — on any doubt (e.g. a name that will not resolve), the
answer is no. The model acts only through tool calls, each validated and
executed by the harness, not the model, so the model cannot bypass the gate.

The `tools.Registry` is generic over the boundary type `B`, and the `agent`
loop only ever needs `Describe` from it — the real checks happen inside each
tool's `Run`. A registry created read-only (`mutate=false`) never exposes a
mutating tool, which `coretest` re-verifies per agent.

## Using it

An agent provides its typed boundary, builds a registry of its tools, and runs
the loop:

```go
client, err := llm.New("MOLE")          // reads MOLE_LLM_BASE_URL / _MODEL / _API_KEY
if err != nil {
    return err
}
reg := tools.NewRegistry(mutate, agentTools...)
res, err := agent.Run(ctx, boundary, client, reg, cfg)
```

See MOLE's `harness/` for a full consumer: the scope gate, the recon/vuln
tools, the embedded technique docs served through `knowledge`, and the report.

## Release flow

Development happens on GitLab. Pushing a `vX.Y.Z` tag mirrors the source — as
one squashed commit per release — to the public GitHub repo. agent-core is a
library, so there is no image to build.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
