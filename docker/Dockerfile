# syntax=docker/dockerfile:1
# Build context is the repo root:  docker build -f docker/Dockerfile -t greyman:latest .
FROM node:24-slim

ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright

# Fonts: Liberation is metric-compatible with Arial/Times/Courier and fontconfig
# aliases those names to it by default; Noto/DejaVu/emoji cover the rest.
RUN apt-get update && apt-get install -y --no-install-recommends \
      xvfb xdotool tini ca-certificates fontconfig x11vnc novnc websockify \
      fonts-liberation fonts-dejavu-core fonts-noto-core fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY daemon/package.json daemon/package-lock.json ./
RUN npm ci --omit=dev \
    && npx playwright install --with-deps --no-shell chromium \
    && rm -rf /var/lib/apt/lists/*

# The build the CLI compares itself with (make image passes VERSION). After the slow
# layers above, so a new version only rebuilds from here.
ARG VERSION=dev
ENV GREYMAN_VERSION=${VERSION}

COPY daemon/src ./src
COPY docker/entrypoint.sh /entrypoint.sh

# Profile (cookies, storage) lives on a volume at /data so it survives `greyman down`.
RUN mkdir -p /data/profile /tmp/.X11-unix \
    && chmod 1777 /tmp/.X11-unix \
    && chown -R node:node /data

USER node
EXPOSE 7777 6080
ENTRYPOINT ["tini", "--", "/entrypoint.sh"]
