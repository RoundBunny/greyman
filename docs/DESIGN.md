# Greyman design

Greyman gives sandboxed coding agents a real browser through a CLI. Goals: token-efficient output, unattended operation, and behavior that looks like one ordinary person browsing (human-paced input, slow per-site rate, real browser headers, the user's own residential IP via Docker NAT). MCP is rejected for its schema and round-trip overhead.

## Architecture

```
greyman (Go, stdlib only)      cmd/greyman, internal/{cli,client,docker,state}
   | HTTP/JSON, Bearer token, 127.0.0.1:<docker-chosen port>
daemon (Node 24, Playwright 1.64)   daemon/src
   | CDP
Chromium, headed on Xvfb 1920x1080   (docker/Dockerfile, node:24-slim, multi-arch)
   | optional, `up --watch`: x11vnc (view-only) -> noVNC on 127.0.0.1:<docker-chosen port>
```

- **Slow commands:** typing and pacing waits can run for minutes. While a command runs, the daemon writes a space every 10 s (leading whitespace is valid JSON); the client has no total timeout and gives up after 60 s of silence. Once a keepalive has gone out the HTTP status is already 200, so the outcome travels in the body's `ok`/`error`. Ctrl-C closes the connection (CLI exits 130, "interrupted"); the daemon sees the hang-up and aborts the action at its next checkpoint: between keystrokes, mouse steps and scroll notches, and while waiting in the rate limiter. An interrupted `type` does not submit. Calls already inside Playwright (`goto`, screenshots, snapshots) run to completion and their result is dropped.
- **Why a daemon:** each CLI call is a separate process; the browser, tabs, cookies and pacing state must outlive it. Rate limiting and humanization live in the daemon so they hold for any client.
- **Why shell out to `docker`:** no SDK dependency. Secrets (token, proxy URL) are passed to `docker run` by env-var name only, so they never appear in `ps`.
- **State:** `$UserConfigDir/greyman/state.json` (0600) holds port, watch port (if enabled) and token. Profile (cookies, storage) lives in the `greyman-profile` volume and survives `down`; `down --purge` deletes it.
- **Live view:** `up --watch` starts x11vnc (`-viewonly`) and websockify serving noVNC from inside the container (`docker/entrypoint.sh`), and publishes port 6080 on loopback. `up` and `status` print a `watch=<url>` with `view_only` and `resize=scale` set. It needs the Xvfb display, so it is refused with `--headless`.

## Install and config

- **Config dir:** `internal/config.Dir()`: `$GREYMAN_CONFIG_DIR`, else `$UserConfigDir/greyman`. It holds `state.json` and `options.cfg`.
- **`options.cfg`:** `KEY=VALUE` lines, `#` comments, written 0600 (it can hold a proxy URL with credentials). `config.Set`, `Update` and `Delete` are a read-modify-write under an `flock` on the directory: they rewrite a key's line in place or append it (`Delete` removes every line for the key), then rename a temp file into place. `greyman install` writes `INSTALLED_AGENTS` (sorted, comma-separated, only ever grows) and `INSTALLED_VERSION` (the binary's version at the last install; recorded, not yet checked). `greyman config` writes the settings below.
- **Settings:** `internal/settings` is the registry of tunable keys: default, validator, and the container variable each one maps to (none for the host-only `IMAGE`, `PLATFORM`, `API_PORT`, `WATCH_PORT`). `Resolve` takes the first value among an `up` flag, `GREYMAN_<KEY>`, `options.cfg`, the default, and validates only that winner. `TZ` and `LOCALE` defaults are detected from the host. `cmdUp` passes every container setting to the daemon as `NAME=value` in docker's environment and `-e NAME` on the command line, so the daemon's own defaults apply only when it runs without the CLI. Keep the registry's defaults equal to `loadConfig`'s. Settings apply on `up`; a running container is not touched, except through `greyman set`. `Display` masks proxy passwords for `config list`/`get`.
- **Live changes:** `greyman set <option> <args>` (`internal/cli/set.go`; gated, since it needs a running browser) validates with the same validators as the config keys, then POSTs `/v1/set`; the daemon applies it to the BrowserContext (`daemon/src/geolocation.js`) and never touches the config. With `--setconfig` the CLI writes the matching keys to `options.cfg` only after the daemon accepted the change. Chromium starts with `--deny-permission-prompts` (`launchArgs`), so no permission prompt is ever shown: it would be browser UI that snap, read and shot cannot see and nothing can answer, and without the flag a pending one also jams that page's later requests. Geolocation is the one permission that can be allowed: `allowGeolocation` sends CDP `Browser.setPermission` `granted` on one browser-level session, and `off` sends `Browser.resetPermissions`, which drops the override. Dropping it, and not setting `prompt`, keeps the state moving from `prompt` to `denied` after a request, so `off` looks like a launch with `USE_GEOLOCATION=0`. Launch only needs CDP when `USE_GEOLOCATION=1`. A missing accuracy falls back to the `GEOLOCATION_ACCURACY` the container started with.
- **`greyman config [list|get|set|unset]`:** ungated. `set` accepts registry keys only and validates first; it refuses a `PROXY` containing `@`. `list` uses `Inspect`, which reports an invalid value instead of failing. The Codex rules file allows every `greyman` subcommand.
- **`greyman install [name]`:** one agent per call (`claude`, `codex`, `cursor`, `copilot`, `all`, or `other [dir]`); no name detects. `internal/agents` maps each agent to files and embeds the skill (`assets/SKILL.md`). Codex, Copilot and Cursor all read `~/.agents/skills/`, so those share one file. Codex also gets a rules file (`$CODEX_HOME/rules/greyman.rules`) because its sandbox blocks the Docker socket, loopback and the state file; it is the only file not overwritten, since users tune it. `other` writes `SKILL.md` and `REQUIREMENTS.md` to a directory for agents greyman does not know.
- **Gate:** `cli.Run` refuses every command except those in `ungated` (`install`, `down`, `config`; `help` returns earlier) until `INSTALLED_AGENTS` is non-empty. New commands are gated by default.
- Agent paths come from vendor docs and are untested on real installs. The Codex rules limits (prefix rules cannot restrict flags; only plain command chains are split) are in `docs/PLAN-install.md`.

## Token strategy

- `snap` is the primary view: Playwright's AI-mode aria snapshot (`page.ariaSnapshot({mode:'ai'})`, public in 1.64) post-processed by `daemon/src/snapshot.js`: hoist nameless wrappers (`generic`, `rowgroup`, `cell`), keep refs only on interactive nodes and named images, fold link URLs onto the link line, drop separator text and empty rows, clip long text. Measured on Hacker News: 47.4k -> 19.5k chars.
- **Dialogs print first.** They sit at the end of the DOM, which is where truncation cuts, so a store picker looked as if it never opened. All outermost `dialog`/`alertdialog` nodes are moved ahead of the page (`snap --modal` prints only them; `read --modal` and `shot --modal` likewise). No attempt is made to find the topmost one: DOM order is not stacking order (in a fixture, a cookie banner followed the real modal), the snapshot carries no modal flag, and elements under a native modal kept their refs. Printing all of them is the choice that needs no heuristic. `shot --modal` has to pick one and takes the largest visible.
- **Filters work on the parsed tree, in the daemon:** `--grep`, `--role`, `--within` (a ref or a line pattern). They keep matches plus their ancestors, and run before the collapse below so a filter still sees every ref. They do not make the browser walk cheaper; every snapshot still calls `ariaSnapshot`. Patterns come from the agent and run on the daemon's only thread, so the filter stage runs under a 1 s `vm` limit: `(a+)+$` on a 34-character line otherwise pinned Node for over 15 s, taking every command down with it.
- **Quoted lines:** Playwright writes a whole line in YAML quotes when a name holds `: ` (`'button "Sale: 50% off" [ref=e4]'`), with inner quotes doubled. Parsed as-is, such an element lost its role and ref and could not be clicked; `parse` unwraps the key and quoted text values first. Scoping the call to a locator is not an option: each `ariaSnapshot` call replaces the frame's ref table (`_lastAriaSnapshotForQuery`), so a scoped call would invalidate every other ref.
- **Dedupe:** runs of ref-less `option` rows past five collapse to `(+N more options)` (the selected one stays); runs of three or more structurally identical siblings collapse to the first plus `(+N similar: refs...)`, so no ref is lost. Signatures and ref lists are taken from the whole tree before anything collapses (`annotate`); comparing a half-collapsed node with a raw one made the result depend on evaluation order. Truncation cuts at a line break and says how to get the rest.
- Actions (`click`, `type`, `open`...) return one status line and report the URL/title only when it changed. `--snap` opts in to a fresh snapshot; every command waits for the page first: `--wait idle` by default, or `text=`/`css=`, or `none` to skip. The wait lasts `DEFAULT_WAIT` (3000 ms) unless `--wait-timeout` says otherwise; a timeout is `wait=timeout`, not an error.
- `read` (readability + turndown markdown) and `shot` (PNG) are explicit fallbacks. `read <ref>` and `read --modal` convert one element's HTML and skip Readability. `shot --max-width N` downscales in the CLI (stdlib box filter); image tokens depend on pixels, not bytes, so JPEG would not help.
- Refs (`e12`, or `f5e5` inside frames) are valid only until the next snapshot. The `f<N>` prefix is a per-document counter, so any navigation changes it.
- **Refs are checked before use.** A ref is bound to a DOM element, not to what it was when snapped, and a single-page app can reuse the element for something else (measured: a button whose label and handler changed was clicked without error). Each `snap` stores `ref -> role, name` on the tab, and `click`/`type`/`read`/`save`/`shot` re-check with `getByRef(ref).and(getByRole(role, {name}))`, which leaves the ref table alone. Refs inside iframes, unnamed nodes and non-interactive roles are not re-checked, so the stored map is dropped when a new document loads (`domcontentloaded`; a same-document navigation keeps it). The check is strict by design: a label that changes after an action (a cart count, a toggle) also makes the old ref "gone or changed" and costs a re-snap. A ref the last snapshot never had, one whose element is gone or renamed, and one from a replaced document each get a different message, and all end in "run snap". `click --name` skips the snapshot: zero or several matches are errors that list what was found.
- **Failures are visible:** `greyman log` reads a per-tab ring buffer (200 entries) fed by Playwright event listeners only: console errors/warnings, uncaught exceptions, failed requests, 4xx/5xx responses and main-frame navigations, with query strings stripped. Nothing is injected into the page, so there is no "origin asked for the position" entry: that would need a wrapper around `navigator.geolocation`, which a page can detect.
- **Images:** `save <ref>` resolves the largest `srcset`/`picture` candidate (else `currentSrc`, else a CSS background) and downloads it by navigating a throwaway tab (`Tabs.scratch`: registered by the context like any page, then forgotten by identity as soon as `newPage()` returns, so a popup opening at the same moment keeps its tab), so headers and TLS are Chromium's own and the request is paced like a navigation. The daemon's own HTTP client would carry Node's TLS fingerprint and break the stance below. A response that is not `image/*` is an error (hotlink protection returns HTML), as is one over 40 MB (the client caps a response at 64 MiB of base64) or served as `Content-Disposition: attachment` (the navigation becomes a download). `save --url` prints a `data:` URL as a short description, not the data.
- **Versions:** the image carries `GREYMAN_VERSION` (the Makefile's `VERSION`, `git describe --tags --always --dirty` by default), the daemon sends it as `X-Greyman-Version` on every response and in `status`, and the CLI warns once on stderr when it differs from its own, or is missing (a container older than this feature). A dirty tree gives the same string for different edits, so it catches a missing rebuild after a commit, not after an uncommitted edit.

## Pacing and input

- `ratelimit.js`: per-origin queue; navigations wait a random 3-6s since the previous hit on that origin, in-page actions 0.4-1.2s. A click that lands on another origin counts as a hit there. Tunable with `GREYMAN_NAV_GAP` / `GREYMAN_ACT_GAP`.
- `humanize.js`: log-normal key delays (median ~90ms, slower after punctuation, occasional pauses); bezier mouse paths with easing and jitter to a random point in the target's box; wheel-notch scrolling. Pure functions take a seedable RNG and are unit tested.
- One in-flight action per tab.
- A tab opened while an action runs (`target=_blank`, `window.open`) becomes active and is reported as `new_tab=`. One that appears on its own (ad, timer) is reported as `bg_tab=` on the next action and stays in the background, so later commands never land on it silently.

## Anti-detection stance

Be a consistent, plausible Linux Chromium user. Fix the environment for real (fonts, 1920x1080 screen, host timezone/locale, automation flags off). Spoof only what cannot be fixed. Never spoof UA or headers: the browser sends its own, which keeps them consistent with its TLS/HTTP2 fingerprint. Not claimed: that this defeats vendors combining fingerprint, behavior and IP reputation.

### Baseline (measured 2026-10-13: M2 Mac, linux/arm64 container, Chromium 156, headed on Xvfb)

| Signal | Value | Verdict |
|---|---|---|
| `navigator.webdriver` | false | fixed by launch args |
| UA | `Chrome/156.0.0.0`, `X11; Linux x86_64` | consistent |
| `navigator.platform` | `Linux x86_64` (even on arm64) | no arch leak |
| `userAgentData.brands` | `Not:A-Brand`, `Chromium` (no "Google Chrome") | tell, TODO |
| H.264 / AAC / WebM `canPlayType` | probably / probably / probably | not a tell on this build |
| WebGL renderer | ANGLE SwiftShader (software) | **largest tell**, no GPU in Docker for Mac |
| Audio devices | 0 | tell |
| Screen / window | 1920x1080 / outer 1919x1079 | 1px off a maximized window |
| Plugins | 5 | normal |
| Timezone | host (America/Chicago) | consistent with egress IP |
| Permissions | `prompt` until a page asks, then `denied`; requests resolve at once (`--deny-permission-prompts`; measured 2026-10-20, headed) | same states as after a user's Block; no human delay |

New headless (`up --headless`) additionally leaks `HeadlessChrome` in the UA and reports an 800x600 screen. Headed is the supported stealth mode.

The font check in the probe script used `document.fonts.check`, which returns true for any family and so measured nothing. Re-measure fonts by comparing rendered text widths against a fallback.

### Roadmap (`daemon/src/stealth.js` TODOs, by expected value)

1. CDP `Runtime.enable` leak (needs a patched driver such as patchright).
2. WebGL vendor/renderer init script covering iframes and workers, with consistent secondary parameters.
3. `userAgentData.brands` "Google Chrome" entry; audio device presence; window outer size.
4. Human handoff for CAPTCHAs. The live view (`up --watch`) is the groundwork but is view-only; letting a person take over mouse and keyboard, and pausing the agent while they do, is the remaining work.

## Security

- The daemon controls a browser holding logged-in cookies. It requires a per-`up` random bearer token (constant-time compare), and Docker publishes the port on `127.0.0.1` only.
- `open` accepts only `http(s)` URLs.
- Proxy credentials go in `GREYMAN_PROXY` (the daemon splits them into Playwright's `username`/`password`; Playwright drops `user:pass@` inside `server`). `up --proxy` rejects a value containing `@`, since arguments are visible in `ps`.
- Chromium runs with `--no-sandbox` as the non-root `node` user; the container is the isolation boundary.
- The live view is also loopback-only and view-only, but it has **no password**: while `--watch` is on, any process or user on the host can see the browser's screen, including logged-in pages. It is off by default.

## Field test: frozen cheesecake (2026-10-13)

Task: find the price, brand, size and complete nutrition facts of a frozen cheesecake at a Chicago grocery store, from one page, plus a photo. Greyman did it on a Mariano's product page (Philadelphia Original Frozen Cheesecake, 24 oz, $12.99 at the Lakeshore East store); the result is in `cheesecake.md`.

What it took: search, switch the store from the site's default (Shorewood, not Chicago) by typing a ZIP, open the product, scroll to lazy-loaded nutrition, close a consent banner, read the full ingredients with `read`, and capture the photo with `shot`. The live view let the user watch it happen. Humanized typing and clicks and ref-based actions worked on a real single-page app; the rate limiter was only measured separately, on example.com. The gaps it exposed are listed below.

## Known limits and open questions

Found in the cheesecake test:

- **`open` returns before a single-page app renders** (at the load event), so the first `snap` can be empty. `snap`, `open` and every action now wait for `idle` by default, capped at `DEFAULT_WAIT` (3000 ms; a chatty page that never goes idle costs the full cap and shows `wait=timeout`), and a site can pass `idle` before its content arrives. `--wait text=|css=` waits on a concrete condition; `--wait none` skips the wait.
- **Snapshots cover the whole page**, 26-30k characters on Mariano's search and product pages, repeating banner and navigation every time. `--grep/--role/--within` and the dedupe cut what an agent reads, not what the browser walks. A viewport-only mode (via `ariaSnapshot({boxes:true})`) is still open.
- **Text is clipped at 200 characters** in snapshots by default (`SNAP_MAX_TEXT`, now configurable), which cut off ingredients; `read` returns the full text. Pick a better default.
- **Clicks can be swallowed by an overlay.** The click point is randomized inside the target, and a floating consent banner covered part of the target, so the click silently did nothing. Check what element sits at the click point and report it, or retry at a clear point.
- **No refs on images** (fixed: named images keep refs and `save <ref>` downloads the original).

Other:

- **Session cookies** (no `Expires`/`Max-Age`) are dropped across `down`/`up`, as in a normal Chrome quit. Persistent cookies survive. Decide whether agents want session cookies kept too.
- Unclean stops (kill, OOM, host crash) leave Chromium's `Singleton*` lock files in the profile. Each container has a new hostname, so Chromium reads them as held by another machine and refuses to start; `docker/entrypoint.sh` deletes them before launch.
- Signal handling: Playwright's own SIGINT/SIGTERM/SIGHUP handlers are disabled; they raced the daemon's shutdown and killed Chromium before it flushed cookies. `scripts/e2e.sh` guards this.
- `greyman up` when already running prints status and ignores new flags (including `--watch`); run `down` first to change mode.
- `BROWSER=chrome` (real Google Chrome, amd64 only) is wired in the Dockerfile and `GREYMAN_CHANNEL` but untested: this host is arm64.
- Navigation caused by a click is paced by the action gap, not the navigation gap.
- `close` on the only open tab closes the browser; the daemon treats that as a crash and exits 1, so the next call fails with an EOF. Filed as a papercut.
- There is no command to choose an option in a `<select>`; its options have no refs and clicking it opens a native popup that no view shows. `type` on the combobox does nothing (checked: the value stayed on the first option). `Locator.selectOption` is the likely basis for a `select <ref> <value>` command.
- Shot of a ref or modal is still screen-sized; there is no `--scale`.
- **Sites failing their own location lookup (not reproduced).** In the Swedish Fish run, shoprite.com ("No stores found") and acmemarkets.com (spinner) failed after `set geolocation`. Rerun on 2026-10-20 with the location set before the page loaded, shoprite.com's "Search Using My Location" returned the stores around Passaic and `greyman log` stayed empty. The first run used an image older than the CLI's `set`, so a location that was never applied or was set after the page read it is the likelier cause. The guess that sites compare the coordinates with the container's IP has no evidence.
