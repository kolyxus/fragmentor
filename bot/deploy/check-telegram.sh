#!/usr/bin/env bash
# Checks from the server, before the first deploy, that the Telegram Bot API
# is reachable and fast enough. See DEPLOY.md, step 2.
#
#   ssh -t fragmentor-vps bash check-telegram.sh
#
# -t gives the script a terminal: without one the token input is not hidden.
# The lines can also be pasted into the server terminal one by one. The bot
# token is read without echo and handed to curl through stdin, so it gets into
# neither the shell history nor the process list.

echo "== server"
uname -m
. /etc/os-release && echo "$PRETTY_NAME"
systemctl --version | head -n 1
id -un

command -v curl >/dev/null || { echo "curl is missing: apt install -y curl"; exit 1; }

echo "== api.telegram.org without the token, 5 attempts"
for i in 1 2 3 4 5; do
  curl -sS -o /dev/null -m 20 -w 'http=%{http_code} connect=%{time_connect}s tls=%{time_appconnect}s total=%{time_total}s\n' https://api.telegram.org/
done

echo "== getMe with the bot token"
read -rs -p 'Bot token (hidden): ' T; echo
printf 'url = "https://api.telegram.org/bot%s/getMe"\n' "$T" | curl -sS -m 20 -K - -w '\ntotal=%{time_total}s\n'
unset T
