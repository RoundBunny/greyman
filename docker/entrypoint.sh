#!/bin/sh
set -eu

# An unclean stop (kill, OOM, host crash) leaves Chromium's profile lock behind. Every
# container gets a new hostname, so Chromium would read the leftover lock as held by
# another machine and refuse to start. Only one container uses the volume at a time.
rm -f "${GREYMAN_PROFILE:-/data/profile}"/Singleton*

# Headed Chromium on a virtual display is the default; GREYMAN_HEADLESS=1 skips X entirely.
if [ "${GREYMAN_HEADLESS:-0}" != "1" ]; then
  export DISPLAY=:99
  rm -f /tmp/.X99-lock
  Xvfb :99 -screen 0 "${GREYMAN_SCREEN_W:-1920}x${GREYMAN_SCREEN_H:-1080}x24" -nolisten tcp &
  i=0
  while [ ! -S /tmp/.X11-unix/X99 ] && [ "$i" -lt 50 ]; do
    i=$((i + 1))
    sleep 0.1
  done

  # `greyman up --watch`: view-only live view of the virtual display, served by noVNC.
  # x11vnc only listens inside the container; the host publishes 6080 on loopback.
  if [ "${GREYMAN_WATCH:-0}" = "1" ]; then
    x11vnc -display :99 -viewonly -nopw -forever -shared -localhost -rfbport 5900 -quiet >/dev/null 2>&1 &
    websockify --web /usr/share/novnc 6080 localhost:5900 >/dev/null 2>&1 &
  fi
fi

exec node /app/src/server.js
