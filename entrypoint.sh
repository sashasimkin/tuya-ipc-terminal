#!/bin/sh
set -u

retry_seconds=${TUYA_START_RETRY_SECONDS:-30}
child_pid=
stopping=0

stop() {
  stopping=1
  if [ -n "$child_pid" ]; then
    kill -TERM "$child_pid" 2>/dev/null || true
    wait "$child_pid" 2>/dev/null || true
  fi
  exit 0
}
trap stop TERM INT

while :; do
  /usr/local/bin/tuya-ipc-terminal "$@" &
  child_pid=$!
  wait "$child_pid"
  status=$?
  child_pid=

  [ "$stopping" -eq 1 ] && exit 0
  echo "Tuya RTSP server exited with status ${status}; retrying in ${retry_seconds}s" >&2
  sleep "$retry_seconds" &
  child_pid=$!
  wait "$child_pid" 2>/dev/null || true
  child_pid=
done
