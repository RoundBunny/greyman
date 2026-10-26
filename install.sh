#!/bin/sh
# greyman installer:
#   curl -fsSL https://github.com/RoundBunny/greyman/releases/latest/download/install.sh | sh
# `make dist` stamps __VERSION__. GREYMAN_VERSION / _BASE_URL / _CONTEXT override for testing.
set -eu

REPO="RoundBunny/greyman"
VERSION="${GREYMAN_VERSION:-__VERSION__}"      # stamped by `make dist`
BASE_URL="${GREYMAN_BASE_URL:-https://github.com/$REPO/releases/download/$VERSION}"
CONTEXT="${GREYMAN_CONTEXT:-https://github.com/$REPO.git#$VERSION}"
GREYMAN_DIR="$HOME/.greyman"                  # bin/greyman, options.cfg, state.json

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

# First of these that is on PATH and writable without sudo; else ~/.local/bin (the caller
# then prints the export line).
pick_link_dir() {
  for d in "$HOME/.local/bin" /usr/local/bin /opt/homebrew/bin; do
    case ":$PATH:" in *":$d:"*) ;; *) continue ;; esac
    if [ -d "$d" ] && [ -w "$d" ]; then echo "$d"; return; fi
    case $d in "$HOME"/*) if mkdir -p "$d" 2>/dev/null; then echo "$d"; return; fi ;; esac
  done
  mkdir -p "$HOME/.local/bin"
  echo "$HOME/.local/bin"
}

# Drop other release images (v* tags) and greyman's untagged leftovers. Runs only once the
# new version is fully installed. A dev greyman:latest and custom tags are left alone, and
# docker refuses to remove an image a running container uses.
clean_images() {
  for tag in $(docker image ls greyman --format '{{.Tag}}'); do
    case $tag in
      "$VERSION") ;;
      v[0-9]*) docker rmi "greyman:$tag" >/dev/null 2>&1 || say "kept greyman:$tag (a container still uses it)" ;;
    esac
  done
  docker image prune -f --filter label=org.opencontainers.image.title=greyman >/dev/null
}

main() {
  case $VERSION in __*) die "unstamped script; set GREYMAN_VERSION" ;; esac

  case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
  case $(uname -m) in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
  asset="greyman-$os-$arch"

  command -v curl >/dev/null || die "curl is required"
  command -v docker >/dev/null || die "docker not found on PATH; install it from https://docs.docker.com/get-docker/"
  docker info >/dev/null 2>&1 || die "docker is installed but its daemon is not reachable; start Docker and re-run"

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  say "downloading greyman $VERSION ($os/$arch)"
  curl -fsSL "$BASE_URL/$asset" -o "$tmp/$asset"
  curl -fsSL "$BASE_URL/checksums.txt" -o "$tmp/checksums.txt"
  want=$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")
  [ -n "$want" ] || die "no checksum for $asset in checksums.txt"
  if command -v sha256sum >/dev/null; then
    got=$(sha256sum "$tmp/$asset" | awk '{ print $1 }')
  else
    got=$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')
  fi
  [ "$got" = "$want" ] || die "checksum mismatch for $asset"

  # Build before installing the binary, so a failed build leaves nothing half-installed.
  if docker image inspect "greyman:$VERSION" >/dev/null 2>&1; then
    say "image greyman:$VERSION already built"
  else
    say "building image greyman:$VERSION (~500 MB download, ~1.2 GB disk, several minutes)"
    DOCKER_BUILDKIT=1 docker build -f docker/Dockerfile --build-arg "VERSION=$VERSION" \
      -t "greyman:$VERSION" "$CONTEXT"
  fi

  bin="$GREYMAN_DIR/bin/greyman"
  mkdir -p "$GREYMAN_DIR/bin"
  chmod 700 "$GREYMAN_DIR" # state.json holds the daemon's bearer token
  chmod +x "$tmp/$asset"
  mv -f "$tmp/$asset" "$bin.new"
  mv -f "$bin.new" "$bin"

  linkdir=$(pick_link_dir)
  link="$linkdir/greyman"
  if [ -e "$link" ] || [ -L "$link" ]; then
    [ "$(readlink "$link" 2>/dev/null)" = "$bin" ] || die "$link exists and is not ours; remove it and re-run"
  fi
  ln -sf "$bin" "$link"
  clean_images

  "$bin" install || say "greyman install did not finish (no agent detected?); run '$bin install all' or '$bin install other [dir]'"

  if [ "$(docker container inspect -f '{{.State.Running}}' greyman 2>/dev/null)" = true ]; then
    say "a greyman container is already running; run 'greyman down' then 'greyman up' to use the new image"
  fi
  case ":$PATH:" in
    *":$linkdir:"*) ;;
    *) say "add $linkdir to your PATH:  export PATH=\"$linkdir:\$PATH\"  (then open a new shell)" ;;
  esac
  say "installed greyman $VERSION -> $link"
}

main "$@"
