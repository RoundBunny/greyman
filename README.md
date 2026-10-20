# greyman

![greyman-logo](greyman-logo-link)

Greyman is a token-efficient browsing tool for coding agents. In short, it's a CLI that controls a Chromium instance inside of a Docker container. Blend in to the crowd and jump over agent web limitations with built-in anti-bot detection and the features of a full web browser. 

Greyman is designed for small to medium-sized tasks that should primarily run in the background, as it uses approximate human-like rate limits by default. Settings live in `options.cfg` in the greyman config directory (`~/Library/Application Support/greyman` on macOS, `~/.config/greyman` on Linux, or `$GREYMAN_CONFIG_DIR`). Manage them with `greyman config` (see Configuration); `greyman install` also records `INSTALLED_AGENTS` and `INSTALLED_VERSION` there. `greyman set` changes some of them on the running browser.

Your agent may occasionally ask you to solve a CAPTCHA if it runs into one. If you don't want to help with these, either ask your agent to skip them or set the config value `SKIP_CAPTCHAS` to true.

Comparison table to other similar tools using a below example scenario
| | browser-sandbox | voidcrawl | greyman | 
|---|---|---|---|
| Token Cost | | | |
| Time to Complete | | | |

```
agent -> greyman (Go CLI) -> HTTP/JSON on 127.0.0.1 -> Node daemon (Playwright) -> Chromium under Xvfb
```

## Quickstart

Requires Docker, Go, and Node. Use this repo only for access to the source code. Otherwise, download this from npm, docker, or the releases page.

```sh
make image build        # build greyman:latest and bin/greyman
export PATH="$PWD/bin:$PATH"

greyman install         # write the skill for the coding agents it detects
greyman up              # start the container (headed Chromium on a virtual display)
greyman up --watch      # same, plus a view-only live view; prints a watch=<url> to open
greyman open news.ycombinator.com
greyman snap            # compact accessibility tree, refs like [e12]
greyman click e12
greyman read            # fallback: page content as markdown
greyman shot            # fallback: PNG screenshot, prints its path
greyman down            # stop; add --purge to also delete cookies/profile
```

`greyman install` writes a skill into each agent's user-level config so the agent knows how to use greyman: `claude`, `codex`, `cursor`, `copilot`, or `all` (one name per call; no name installs for the agents it detects). For any other agent, `greyman install other [dir]` writes `SKILL.md` and `REQUIREMENTS.md` (what the host must permit) into `dir` (default: the current directory). Codex also gets `~/.codex/rules/greyman.rules`; an existing file is kept unless you pass `--force`. Until at least one agent is installed, every command except `help`, `install`, `down` and `config` refuses to run.

`greyman help` lists every command. Action commands print one status line and accept `--tab tN` and `--snap` (append a fresh snapshot).

`--watch` serves noVNC on `127.0.0.1` only, view-only, headed mode only (it cannot be combined with `--headless`). Run `greyman down` before `up` to change modes: `up` on a running container prints its status and ignores new flags.

## Using it from an agent

- Refs from `snap` look like `e12`, or `f5e5` for elements inside frames. They are valid until the next snapshot or navigation; a miss prints `ref ... not found or not actionable; run snap`.
- Flags may come before or after arguments. Use `--` before text that starts with `-`: `greyman type e3 --submit -- "-5"`.
- `snap` covers the whole page and can run to tens of thousands of characters. Cap it with `--max N`, or filter it (`greyman snap | grep -i price`).
- `snap` clips long text at 200 characters by default (`SNAP_MAX_TEXT`). Use `read` when you need the full text.
- Single-page apps may not be rendered when `open` returns. If a snapshot is empty, wait a few seconds and run `snap` again.
- `new_tab=tN` in a status line: the action opened a tab and it is now active. `bg_tab=tN`: a tab opened on its own (popup, ad) and stays in the background; `close --tab tN` dismisses it.
- Clicks land at a randomized point inside the target. A cookie or consent banner floating over it can swallow the click: close the banner first.
- Navigations to one origin are spaced 3-6 s apart and actions 0.4-1.2 s (see Configuration), so multi-step tasks take a while by design.

## Known limitations

- Whole-page snapshots, 200-character text clipping (now adjustable with `SNAP_MAX_TEXT`), `open` returning before the page renders, clicks that fail silently when covered, and no refs on images (the photo in `cheesecake.md` was captured through its clickable gallery element). All found during the cheesecake test and all still open.
- Session cookies (no expiry) are dropped across `down`/`up`, as in a normal Chrome quit. Cookies with an expiry survive.
- `BROWSER=chrome` (real Google Chrome, amd64 only) is untested.
- `--headless` leaks `HeadlessChrome` in the user agent and an 800x600 screen. Headed is the supported mode.

## Tests

```sh
make deps    # npm ci for the daemon (first time)
make test    # gofmt, go vet, go test, node --test
./scripts/e2e.sh   # 21-check smoke test against a real container; needs network, wipes the profile
```

## Configuration

Settings live in `options.cfg` (`KEY=VALUE` lines, `#` comments) in the config dir: `~/Library/Application Support/greyman` on macOS, `~/.config/greyman` on Linux, or `$GREYMAN_CONFIG_DIR`. Manage them with `greyman config`:

```sh
greyman config                        # every setting, its value, and where the value came from
greyman config set NAV_GAP 1000-2000  # validated, then written to options.cfg
greyman config get NAV_GAP
greyman config unset NAV_GAP          # back to the default
```

Each setting takes the first value found in: a flag of `greyman up`, the `GREYMAN_<KEY>` environment variable, `options.cfg`, then the default. A value that fails validation stops `up` and names the key and where it came from. Changes apply on the next `up`; `up` on a running container ignores them, so run `down` first. The exception is `greyman set` (below), which changes some settings on a running container.

| Key | Default | Meaning |
|---|---|---|
| `NAV_GAP` | `3000-6000` | ms between navigations to one origin (random in range) |
| `ACT_GAP` | `400-1200` | ms between in-page actions on one origin |
| `ACTION_TIMEOUT_MS` | `5000` | ms to wait for an element before an action fails |
| `NAV_TIMEOUT_MS` | `30000` | ms to wait for a page to load |
| `MAX_CHARS` | `24000` | cap on `snap`/`read` output |
| `SNAP_MAX_TEXT` | `200` | `snap` clips any one text to this many characters; `read` is not clipped |
| `PROXY` | none | upstream proxy URL (`--proxy`) |
| `LOCALE` | from `LANG` | browser locale such as `en-US` |
| `TZ` | from the host | browser time zone, an IANA name |
| `USE_GEOLOCATION` | `0` | `1` reports the coordinates below to pages that ask for the location; `0` denies every request |
| `GEOLOCATION_COORD_LAT`, `GEOLOCATION_COORD_LON` | `0`, `0` | reported latitude (-90 to 90) and longitude (-180 to 180) |
| `GEOLOCATION_ACCURACY` | `10` | reported accuracy radius in meters |
| `HEADLESS` | `0` | `1` runs Chromium headless (`--headless`) |
| `WATCH` | `0` | `1` serves the live view (`--watch`); headed only |
| `SCREEN_W`, `SCREEN_H` | `1920`, `1080` | browser window size in pixels |
| `API_PORT` | `0` | host port for the daemon API; `0` lets Docker pick |
| `WATCH_PORT` | `0` | host port for the live view; `0` lets Docker pick |
| `IMAGE` | `greyman:latest` | Docker image to run (`--image`) |
| `PLATFORM` | image's own | container platform such as `amd64` (`--platform`) |

Notes:
- Both ports are published on `127.0.0.1` only. `WATCH_PORT` is ignored unless `WATCH=1`.
- `PROXY` may include `user:pass@`. Add that line to `options.cfg` yourself (the file is written 0600): `config set` and `--proxy` refuse credentials because arguments show in `ps` and shell history. `config list` and `get` mask the password.
- The settings except `IMAGE`, `PLATFORM` and the two ports reach the daemon as environment variables (`GREYMAN_NAV_GAP`, and so on; `TZ` for the time zone).

### Changing things on the running browser

`greyman set <option> <args>` changes a setting on the running browser at once. It does not touch `options.cfg` unless you add `--setconfig`, which saves the matching keys after the browser has accepted the change. Use `config set` when nothing is running.

```sh
greyman set geolocation 40.7128 -74.0060            # live only; accuracy defaults to GEOLOCATION_ACCURACY
greyman set geolocation 48.8566 2.3522 25 --setconfig   # also saves USE_GEOLOCATION=1, the coordinates and the accuracy
greyman set geolocation off                         # deny every location request; --setconfig saves USE_GEOLOCATION=0
```

- Geolocation applies to every tab. A page that already read the position keeps it until it asks again, so reload it.
- `off` is the same as starting with `USE_GEOLOCATION=0`: every location request is denied at once (`PERMISSION_DENIED`). Pages see the permission as `prompt` until they ask and `denied` after, as for a user who clicked Block.
- Chromium starts with `--deny-permission-prompts`, so every permission prompt (notifications, camera, microphone, clipboard and so on) is denied at once, whether or not location is allowed. A prompt is browser UI that `snap`, `read` and `shot` cannot show and nothing could answer. Geolocation is the only permission greyman can allow.
- `--setconfig` warns when a `GREYMAN_<KEY>` environment variable is set for a key it saved: the environment outranks `options.cfg`, so the next `up` would ignore the saved value.
- This only sets what the browser API reports. The IP address that sites see is unchanged, so pick coordinates that fit your `PROXY`, `TZ` and `LOCALE`.
- `up` warns when `USE_GEOLOCATION=1` and both coordinates are still `0`.

The image is built with `make image`; `BROWSER=chrome` and `PLATFORM=linux/amd64` select real Chrome on amd64.

Design notes and the anti-detection roadmap: [docs/DESIGN.md](docs/DESIGN.md).
