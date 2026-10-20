# What greyman needs from the agent host

`greyman` is a command-line program. Whatever runs it for your agent has to permit the following.

1. Execute the `greyman` binary on the host (not inside a sandbox that hides Docker or loopback).
2. `up` and `down` run the `docker` CLI and need access to the Docker daemon socket. The environment passes through to `docker` (`HOME`, `DOCKER_HOST`, `DOCKER_CONFIG`).
3. Every other command makes HTTP calls to `127.0.0.1` on a port Docker picks at each `up`.
4. Read and write `state.json` and `options.cfg` in the greyman config directory: `~/Library/Application Support/greyman` on macOS, `~/.config/greyman` on Linux, or `$GREYMAN_CONFIG_DIR` if set.
5. `shot` writes a PNG to the temp directory, or to the path given with `-o`.
6. greyman reads `GREYMAN_*`, `TZ`, `LC_ALL` and `LANG` from the environment.
7. Internet traffic comes from the container, not the agent's process, so the agent's own network access can stay restricted.
8. Commands can block for minutes (per-origin pacing, a 30 second navigation timeout, human-paced typing that takes about 10 seconds per 100 characters). There is no total time limit: the daemon sends a keepalive every 10 seconds and the client gives up only after 60 seconds of silence. Use a tool timeout of at least 3 minutes.
9. Suggested approval policy: auto-approve every command except `up` and `down`, which start and remove a container (`down --purge` also deletes the cookie profile).

`SKILL.md` in this directory tells an agent how to use greyman. Put it wherever your agent loads skills or instructions from.
