# syntax=docker/dockerfile:1
# Build context is the repo root:  docker build -f docker/Dockerfile -t greyman:latest .
FROM node:24-slim

# chromium: Playwright's Chromium (works on arm64 and amd64).
# chrome:   Google Chrome stable (amd64 only; Google ships no Linux arm64 build).
ARG BROWSER=chromium
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright \
    GREYMAN_CHANNEL=${BROWSER}

# Fonts: Liberation is metric-compatible with Arial/Times/Courier and fontconfig
# aliases those names to it by default; Noto/DejaVu/emoji cover the rest.
RUN apt-get update && apt-get install -y --no-install-recommends \
      xvfb tini ca-certificates fontconfig x11vnc novnc websockify \
      fonts-liberation fonts-dejavu-core fonts-noto-core fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY daemon/package.json daemon/package-lock.json ./
RUN npm ci --omit=dev \
    && if [ "$BROWSER" = "chromium" ]; then FLAGS="--no-shell"; else FLAGS=""; fi \
    && npx playwright install --with-deps $FLAGS "$BROWSER" \
    && rm -rf /var/lib/apt/lists/*

COPY daemon/src ./src
COPY docker/entrypoint.sh /entrypoint.sh

# Profile (cookies, storage) lives on a volume at /data so it survives `greyman down`.
RUN mkdir -p /data/profile /tmp/.X11-unix \
    && chmod 1777 /tmp/.X11-unix \
    && chown -R node:node /data

USER node
EXPOSE 7777 6080
ENTRYPOINT ["tini", "--", "/entrypoint.sh"]
