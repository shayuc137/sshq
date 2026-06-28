<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Project

sshq — 面向 AI agent 的跨平台 SSH 管理 CLI。Go 单二进制，通过 GitHub Releases 分发。

核心能力：exec（含 script-file）、cp（SFTP + raw fallback + relay）、config CRUD、cluster 并发、tunnel（local/remote forward）、daemon 连接池、probe、跨 shell 探测与编码转码。

基于 [badseal/ssh-skill](https://github.com/badseal/ssh-skill) 的行为参考，Go 重写。

## Code Discovery: MCP Tools First

This project has two MCP code search tools configured. Use them before falling back to `rg`/`Read`.

First call: run `list_projects()` to get the project name, or `index_repository(repo_path=".")` if not indexed.

### When to use what

**Before modifying code** — check who calls it:
```
trace_path(function_name="Dial", direction="inbound", depth=3)
```
One query returns the full caller chain. Don't rg 3 times manually.

**Finding code when you don't know the keyword**:
```
mcp__ace-tool__search_context(query="how does the daemon handle file transfer progress")
```
Matches semantically, even when the code uses different terminology.

**Reading a function with its call context**:
```
get_code_snippet(qualified_name="...sshclient.dialViaProxy", include_neighbors=true)
```
Returns source + callers + callees in one call.

**Structural questions** (fan-out, dead code, complexity):
```
query_graph(query="MATCH (f:Function) WHERE f.complexity >= 10 RETURN f.name, f.file, f.complexity ORDER BY f.complexity DESC")
```

**Starting a new session or unfamiliar area**:
```
get_architecture(project="home-shayu-project-sshq")
```

**Known keyword, want enriched results**:
```
search_code(pattern="resolveHost", mode="compact")   # grep + dedup to function level + in/out degree
search_graph(query="handle tunnel")                   # BM25 ranked, structural boost
```

### When rg is fine

- Exact string match in non-code files (configs, docs, comments)
- Simple one-shot symbol lookup where you already know the file
- Searching inside `.trellis/` or other metadata directories

### Semantic search: use ace-tool

For "find code that does X" queries, always use `ace-tool search_context` — it matches against actual code content. `codebase-memory search_graph(semantic_query=...)` only compares function name embeddings and produces poor results on Go code.

`codebase-memory search_graph(query=...)` (BM25 mode) is fine for keyword search with structural ranking.

### What doesn't work well

- `detect_changes` right after indexing — it compares index vs working tree, so no diff if index is fresh

## Skill Documentation Sync

When modifying CLI commands (add/remove commands, change flags, update usage/descriptions) or sshq metadata format, regenerate docs before committing:

```bash
sshq docs --skill skills/sshq/references/    # skill reference docs
```

If a top-level command was added or removed, also update the routing table in `skills/sshq/SKILL.md`.

After changes to skill files, run `sshq skill install` to update the local installation.

Full rules: `.trellis/spec/go/cli-conventions.md` → "Command Documentation Sync".
