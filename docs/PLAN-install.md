# Plan: `greyman install`

Status: implemented. Agent paths below come from vendor docs and are untested on real installs. The Codex rules file has not been run through `codex execpolicy check` (codex is not installed on the dev machine).

## Goal

`greyman install [claude|codex|cursor|copilot|all|other]` sets greyman up for coding agents:

1. Writes a skill (and, for Codex, a permission rules file) into each agent's user-level config.
2. Records which agents are done in a config file.
3. Every other command refuses to run until at least one agent is recorded.

Running `install` is the user's opt-in, so it writes without extra confirmation. The request spelled Cursor as `cursur`; the plan reads it as `cursor`.

## Decisions

| # | Decision |
|---|---|
| 1 | Config file lives in the config dir as `KEY=VALUE` (below). |
| 2 | One agent per call. More than one name is a usage error. `all` counts as one name. |
| 3 | `greyman install other [dir]` is the path for agents greyman does not know. It writes `SKILL.md` and `REQUIREMENTS.md` into `dir`. |
| 4 | Codex rules allow every `greyman` command, including `up`, `down` and `config`. (Originally only `up` and `down` prompted.) |
| 5 | `INSTALLED_VERSION` is recorded at install. Nothing checks it yet (no warning). |

## Behavior

```
greyman install                  # detect agents on this machine, install for those
greyman install codex            # one agent per call
greyman install all              # claude, codex, cursor, copilot (detected or not)
greyman install other [dir]      # SKILL.md + REQUIREMENTS.md into dir (default: cwd)
```

- Idempotent. Re-running refreshes files and unions the config (never removes agents).
- No argument and nothing detected: exit 1, print the supported names, `greyman install all` and `greyman install other`. Never install to everything silently. Detection never includes `other`, and `all` does not include it.
- Output follows the existing status-line style, one line per agent, then a summary:
  `ok claude skill=/Users/x/.claude/skills/greyman/SKILL.md`
  `ok codex skill=... rules=/Users/x/.codex/rules/greyman.rules`
  `ok other skill=SKILL.md requirements=REQUIREMENTS.md` (paths print as joined with `dir`)
  `ok installed=claude,codex`
- Exit codes match `cli.Run`: 0 ok, 1 any write failed, 2 usage error (unknown name, more than one name, a `dir` given to anything but `other`).
- Unix only for now (macOS, Linux). Paths use `os.UserHomeDir()` and `filepath.Join`, so Windows is not blocked, just untested.

## Agent targets

Key finding: `~/.agents/skills/` is read by Codex, Copilot and Cursor. One file covers three agents.

| Agent | Skill written to | Extra | Detected if (any) |
|---|---|---|---|
| claude | `~/.claude/skills/greyman/SKILL.md` | none | `claude` on PATH; `~/.claude` exists |
| codex | `~/.agents/skills/greyman/SKILL.md` | `rules/greyman.rules` under `$CODEX_HOME` (default `~/.codex`) | `codex` on PATH; `~/.codex` or `$CODEX_HOME` exists |
| copilot | `~/.agents/skills/greyman/SKILL.md` | none | `copilot` on PATH; `~/.copilot` or `$COPILOT_HOME` exists |
| cursor | `~/.agents/skills/greyman/SKILL.md` | none | `cursor` or `cursor-agent` on PATH; `/Applications/Cursor.app`; `~/.cursor` exists |
| other | `<dir>/SKILL.md` | `<dir>/REQUIREMENTS.md` | never detected |

Sources: Codex skills and rules docs; GitHub Copilot agent-skills page (personal skills: `~/.copilot/skills`, `~/.agents/skills`); Cursor skills page (user skills: `~/.agents/skills/`, `~/.cursor/skills/`; also reads `~/.claude/skills/` and `~/.codex/skills/`). Claude Code's own docs were not re-checked this session.

Notes:
- Targets are deduplicated by directory. Because it is one agent per call, running `codex` then `cursor` rewrites the same `~/.agents/skills/greyman/SKILL.md`. Harmless, and the no-argument detect path writes it once.
- Cursor also scans `~/.claude/skills/`. A machine with Claude Code and Cursor will have the skill at two paths. Whether Cursor shows a duplicate is untested (Codex docs say same-name skills are not merged).
- Copilot detection in VS Code (extension only, no CLI) is a guess: `~/.vscode/extensions/github.copilot-*`. Undocumented.
- Not researched: Copilot and Cursor command-approval or sandbox behavior. Codex needs the rules file because its default sandbox blocks the Docker socket, loopback and the state file. The other two may need an equivalent.

### `install other [dir]`

For any agent greyman does not know. It writes two files into `dir` (default cwd, created if needed) and records `other` in `INSTALLED_AGENTS`, which satisfies the gate. The `dir` value is not recorded. Both files are always rewritten.

- `SKILL.md`: the same content the other agents get.
- `REQUIREMENTS.md`: an agent-neutral list of what the host has to permit, derived from the code:
  1. Execute the `greyman` binary on the host.
  2. `up` and `down` run the `docker` CLI and need access to the Docker daemon socket. The environment passes through to `docker` (`HOME`, `DOCKER_HOST`, `DOCKER_CONFIG`).
  3. Every other command makes HTTP calls to `127.0.0.1` on a port Docker picks at each `up`.
  4. Read and write `state.json` and `options.cfg` in the config dir (`~/Library/Application Support/greyman` on macOS, `~/.config/greyman` on Linux, or `$GREYMAN_CONFIG_DIR`).
  5. `shot` writes a PNG to the temp dir, or to the `-o` path.
  6. Reads `GREYMAN_*`, `TZ`, `LC_ALL` and `LANG` from the environment.
  7. Internet traffic comes from the container, not the agent's process, so the agent's own network can stay restricted.
  8. Commands can block for minutes (per-origin pacing, 30 s navigation timeout, human-paced typing). There is no total time limit: the daemon sends a keepalive every 10 s and the client gives up only after 60 s of silence. Use a tool timeout of at least 3 minutes.
  9. Suggested approval policy: auto-approve everything except `up` and `down`, which start and remove a container (`down --purge` also deletes the cookie profile).

### Codex rules file

Codex `allow` rules run the command outside the sandbox. greyman needs that for loopback, the Docker socket and `state.json`. Written to `$CODEX_HOME/rules/greyman.rules` (default `~/.codex/rules/`):

```python
prefix_rule(
    pattern = ["greyman"],
    decision = "allow",
    justification = "greyman drives a local Docker browser: needs loopback, the Docker socket and its state file outside the sandbox",
    match = ["greyman snap", "greyman click e12", "greyman up --watch", "greyman down --purge", "greyman config set NAV_GAP 1000-2000"],
)
```

Limits:
- Prefix rules cannot restrict flags, and this one allows every subcommand with no prompt. `shot -o /any/path` writes a PNG anywhere the user can, outside the sandbox, `up --image X` runs any image, `down --purge` deletes the cookie profile, and `config set` can change pacing and the proxy.
- Codex splits only plain `&&`/`||`/`;`/`|` chains. Redirects, `$(...)` and env-var prefixes will not match the `greyman` prefix. The skill tells agents to issue plain `greyman ...` commands.
- Rules are experimental per the Codex docs.
- Verify with `codex execpolicy check --pretty --rules <file> -- greyman snap`.

Overwrite policy: skill files and `REQUIREMENTS.md` are greyman's own and are always rewritten. The rules file is user-tunable, so it is written only if absent. If it exists and differs, print `kept existing rules=<path>`; `--force` replaces it.

## Config file

README already promises `options.cfg` and a future `greyman config <option> <value>`; neither exists. This feature creates the file and its read/write code. The `greyman config` command itself is out of scope.

- Location: `<config dir>/options.cfg`, the same directory as `state.json`. `GREYMAN_CONFIG_DIR` overrides it (tests already use that). Not beside the executable as the README says: brew's Cellar path changes on every upgrade and plugin roots are replaced on update.
- Format: `KEY=VALUE` lines; `#` comments and blank lines ignored. Standard library only (`bufio.Scanner`, `strings.Cut`). `Set` rewrites the matching line in place or appends, keeping comments, then atomic temp-file + rename (same pattern as `state.Save`).
- Keys written: `INSTALLED_AGENTS=claude,codex` (sorted, comma-separated; `other` is one of the values), set to the union of the old value and this run's agents; and `INSTALLED_VERSION`, the binary's `internal/version.Version` (`dev` unless stamped with `-ldflags -X`, see the Makefile).
- A new file starts with a one-line `#` header.

## Gate

In `cli.Run` before dispatch: if `INSTALLED_AGENTS` is missing or empty, exit 1 with

`greyman is not set up for any agent; run `greyman install``

Exempt: `help`, `install`, `down` (so a user can still stop a running container if the config is lost). Implemented as an `ungated` name set in `cli.go` rather than a field on `command` (a field would have forced an edit to every command literal); new commands are still gated by default.

## Code layout

Follows `internal/{cli,client,docker,state}`. No new dependencies.

- `internal/config`: `Dir()`, `Load`, `Get`, `Set`. `state` calls `config.Dir()` so the directory logic lives in one place.
- `internal/agents`: registry of `{Name, Detect, Targets(home, dir)}`. Home dir, env and `dir` are passed in, so everything is testable with a temp HOME. `//go:embed` for `assets/SKILL.md`, `assets/REQUIREMENTS.md` and `assets/greyman.rules`.
- `internal/cli/install.go`: `cmdInstall` using `newFlags`/`parse`/`usageError`, plus a `--force` flag. Registered in `commands()` (`cli.go:30`), so `greyman help` lists it for free.

Install steps:
1. Parse. 0 positionals: detect. 1 positional: a known name. 2 positionals: only if the first is `other`. Anything else: usage error. `all` expands to the four agents.
2. Map agents to deduplicated targets.
3. For each target: `MkdirAll` (0755), write atomically.
4. Union the agent into `INSTALLED_AGENTS` only if all of its targets succeeded.
5. Print lines; exit 1 if any failed.

## Skill content (`SKILL.md`)

One file for all agents. Frontmatter `name: greyman` (matches the folder, which Cursor requires) and a `description` that says when to use it. Body is version-stable guidance only; command syntax defers to `greyman help`.

- When to use greyman: a real browser, logged-in sessions, JS-heavy pages.
- `snap` first; `shot` only when visuals matter; `read` for full text.
- Cap large output with `--max N` or grep. `--snap` only when new refs are needed.
- Refs die on navigation or the next snap. Close banners before clicking.
- Pacing is deliberate: batch work, run in the background.
- Run plain `greyman ...` commands: no `$(...)`, redirects or env-var prefixes (Codex rules).
- No token-cost numbers until they have been measured with `count_tokens`.

## Tests

- Go, table-driven, `t.Setenv("HOME", tmp)` and `GREYMAN_CONFIG_DIR`:
  - file contents and paths per agent, plus dedup
  - idempotency, and config union
  - detection via fake dirs and a fake `PATH`
  - usage errors: unknown name, two names, `dir` with a non-`other` agent
  - none detected
  - `other`: default cwd (use `t.Chdir`), custom dir, records `other`, rewrites both files
  - gate: blocks, and exempts `help`/`install`/`down`
  - rules: written only if absent, `--force` replaces
- `scripts/e2e.sh`: export a temp `GREYMAN_CONFIG_DIR`, assert `greyman up` fails before install, then `greyman install other "$(mktemp -d)"` and assert both files plus `INSTALLED_AGENTS=other`. This needs no `HOME` override, so Docker still finds `~/.docker`.
- `make test` stays green: gofmt, vet, go test, node tests.

## Docs to update

- README quickstart: add `greyman install` before `up`. Fix the `options.cfg` location and say `greyman config` is not built yet.
- `docs/DESIGN.md`: install and config sections.

## Out of scope

`greyman uninstall`, `greyman config`, per-project (repo-level) installs, Windows, packaging (brew, plugin).

## Open questions and assumptions

1. Record the installed greyman version too, and warn when the binary is newer than the installed skill?
A) Yes, record in config. Resolved: record only, no warning yet (revisit with `greyman version` and the daemon-version check).
2. Does Cursor list the skill twice when both `~/.claude/skills` and `~/.agents/skills` have it? Needs a real test.
3. Resolved: `install other` with no `dir` writes to the current directory.
4. Assumed: the `other` files are named `SKILL.md` and `REQUIREMENTS.md` (the standard skill filename), not lowercase.
5. Assumed: `dir` is a positional argument, as in your example, not a flag.
