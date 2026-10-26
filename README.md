# greyman

Greyman is a token-efficient browsing tool for coding agents. Agents control a Chromium instance in a Docker container through a CLI. Blend in to the crowd and jump over agent web limitations with built-in anti-bot detection and the features of a full web browser. 

Greyman is designed for small to medium-sized tasks that should primarily run in the background, as it uses approximate human-like rate limits by default. Settings live in `~/.greyman/options.cfg`. Manage them with `greyman config` (see Configuration); `greyman install` also records `INSTALLED_AGENTS` and `INSTALLED_VERSION` there. `greyman set` changes some of them on the running browser.

## Quick Install
Requires Docker.

Mac/Linux: 
\$ `curl -fsSL https://github.com/RoundBunny/greyman/releases/latest/download/install.sh | sh`

Windows version coming in a later release!

## Token Efficiency Comparisons

Task: Go to the wikipedia page for "Web Browser" using only `<tool>`. How many languages is the page available in?
| Tool | ccusage measured tokens | ccusage API Cost | Claude CLI token count | 
|---|---|---|---|
| Claude Web Fetch (fresh startup) | 127,690 | $0.07 | 450 | 
| Claude Web Fetch (subsequent run) | 86,163 | $0.01 | 350 |
| greyman shot (fresh startup) | 268,083 | $0.11 | 650 |
| greyman shot (subsequent run) | 194,170 | $0.03 | 300 |
| greyman snap (fresh startup) | 220,315 | $0.09 | 450 |
| greyman snap (subsequent run) | 136,109 | $0.02 | 190 |

Task: Using only `<tool>`, find out how much a dozen eggs cost from any Walmart in Austin. Only source information from Walmart, and don't use any external sites.
| Tool | ccusage measured tokens | ccusage API Cost | Claude CLI token count | Usage% | Successful? | 
|---|---|---|---|---|---|
| Web Fetch/Search | 277,290 | $0.13 | 1.7k | <0.5% | No, blocked by site |
| Claude in Chrome | N/A | N/A | N/A | ~2% | Yes |
| raw Playwright | 584,530 | $0.35 | 5.8k | ~1% | Blocked on headless, successful on headed after a few attempts |
| greyman | 415,085 | $0.15 | 950 | <0.5% | Yes | 

Web Fetch or Search tend to be cheaper, but are blocked on a large amount of websites. When blocked, Claude tends to use cached results or info from third party sites unless explicitly told not to. This can easily result in misinfo or wasted token expenditure through unnecessary extra queries. Claude in Chrome is far too costly to use for independent tasks, and it has to run in a window. Playwright alone tripped bot detection all but one time out of 7 attempts, and was significantly slower and more expensive than greyman.

## Building from Source

Requires Docker, Go, and Node.

```
make image build        # build greyman:latest and bin/greyman
```

## Configuration

Config file is `~/.greyman/options.cfg`. Manage settings for later launches with `greyman config`. Use `greyman set` to change the values for the live session, appending the flag `--setconfig` to keep them. `greyman up` flags and `GREYMAN_<KEY>` environment variables override config values.

```sh
greyman config                        # prints all config values
greyman config set NAV_GAP 1000-2000  # 
greyman config get NAV_GAP            #
greyman config unset NAV_GAP          # resets value to default
```
