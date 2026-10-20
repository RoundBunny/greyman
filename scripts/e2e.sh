#!/usr/bin/env bash
# Manual smoke test against a real container. Needs docker, `make image build`, and
# network access to example.com and httpbin.org. Wipes the greyman profile volume.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD/bin:$PATH"
# Own config dir, so the run starts without an install and leaves the real one alone.
export GREYMAN_CONFIG_DIR="$(mktemp -d)"
skill_dir="$(mktemp -d)"
# Stop the container before its state file goes, or a failed run leaves it running
# with no way for the next `greyman up` to talk to it.
trap 'greyman down >/dev/null 2>&1 || true; rm -rf "$GREYMAN_CONFIG_DIR" "$skill_dir"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
expect() { # expect <description> <haystack> <needle>
  [[ "$2" == *"$3"* ]] || fail "$1: wanted '$3' in: $2"
  echo "ok   $1"
}
ref_of() { greyman snap | grep -E "$1" | grep -oE '\[[a-z0-9]+\]' | head -1 | tr -d '[]'; }

greyman down --purge >/dev/null
expect "gate before install" "$(greyman up 2>&1 || true)" "run \`greyman install\`"
expect "install other" "$(greyman install other "$skill_dir")" "ok installed=other"
[[ -f "$skill_dir/SKILL.md" && -f "$skill_dir/REQUIREMENTS.md" ]] || fail "install other: files missing in $skill_dir"
expect "install config" "$(cat "$GREYMAN_CONFIG_DIR/options.cfg")" "INSTALLED_AGENTS=other"
expect "up" "$(greyman up)" "browser=chromium"

expect "open" "$(greyman open example.com)" 'title="Example Domain"'
ref=$(ref_of 'link "Learn more"')
expect "click navigates" "$(greyman click "$ref")" "iana.org"
expect "back" "$(greyman back)" "url=https://example.com/"
expect "read" "$(greyman read)" "# Example Domain"

png=$(greyman shot)
[[ "$(head -c 4 "$png" | tail -c 3)" == "PNG" ]] || fail "shot is not a PNG: $png"
echo "ok   shot"

# Same-origin rate limit: second open must be delayed by the 3s minimum gap.
greyman open example.com >/dev/null
start=$SECONDS
greyman open example.com >/dev/null
(( SECONDS - start >= 2 )) || fail "rate limit: second open returned after $((SECONDS - start))s"
echo "ok   rate limit"

greyman open https://httpbin.org/forms/post >/dev/null
ref=$(ref_of 'textbox "Customer name')
greyman type "$ref" "Ada Lovelace" >/dev/null
expect "type" "$(greyman snap)" "Ada Lovelace"
expect "stale ref" "$(greyman click e999 2>&1 || true)" "run snap"

# A persistent cookie must survive down/up; purge must clear it.
greyman open 'https://httpbin.org/response-headers?Set-Cookie=keep%3D1%3B%20Max-Age%3D3600%3B%20Path%3D%2F' >/dev/null
greyman down >/dev/null
greyman up >/dev/null
greyman open https://httpbin.org/cookies >/dev/null
expect "cookie survives down/up" "$(greyman read)" '"keep"'
greyman down --purge >/dev/null
greyman up >/dev/null
greyman open https://httpbin.org/cookies >/dev/null
[[ "$(greyman read)" != *'"keep"'* ]] || fail "cookie survived --purge"
echo "ok   purge clears profile"

port=$(docker port greyman 7777/tcp | head -1)
expect "loopback only" "$port" "127.0.0.1:"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://$port/v1/status")
[[ "$code" == 401 ]] || fail "unauthenticated status returned $code"
echo "ok   auth"

# Settings in options.cfg reach the daemon on the next up. Uses host port 47777.
greyman down >/dev/null
greyman config set NAV_GAP 0-0 >/dev/null
greyman config set SNAP_MAX_TEXT 20 >/dev/null
greyman config set MAX_CHARS 60 >/dev/null
greyman config set API_PORT 47777 >/dev/null
expect "config get" "$(greyman config get MAX_CHARS)" "60"
greyman up >/dev/null
expect "API_PORT" "$(docker port greyman 7777/tcp | head -1)" "127.0.0.1:47777"
greyman open example.com >/dev/null
start=$SECONDS
greyman open example.com >/dev/null
(( SECONDS - start < 3 )) || fail "NAV_GAP=0-0: second open took $((SECONDS - start))s, the default gap is 3s or more"
echo "ok   NAV_GAP"
snap=$(greyman snap)
(( ${#snap} < 100 )) || fail "MAX_CHARS=60: snap is ${#snap} characters"
expect "MAX_CHARS" "$snap" "[truncated"
expect "SNAP_MAX_TEXT" "$(greyman snap --max 5000)" "..."

greyman down >/dev/null
echo "all passed"
