#!/bin/sh
# Installs a new bot binary on the server: section 4 of bot/DEPLOY.md.
# make deploy copies this script and the binary to the home directory of
# root on fragmentor-vps and runs, over ssh:
#
#   sh install-release.sh <git revision> <binary file name>
#
# It stops the bot, copies the database to /var/backups/fragmentor-<time>
# (keeping the last 10 copies), keeps the old binary as fragmentor.prev, puts
# the new one in place, starts the bot and waits until its log shows the
# expected version polling Telegram. If anything fails while the bot is
# stopped, it starts the bot again. The script and the binary are removed
# from the home directory either way.
#
# The FRAGMENTOR_* variables only let tests run it outside the server.
set -eu

revision=${1:?usage: sh install-release.sh <git revision> <binary file name>}
binary=${2:?usage: sh install-release.sh <git revision> <binary file name>}
version=$(printf '%.12s' "$revision") # the bot logs 12 characters
new="$HOME/$binary"
bin=${FRAGMENTOR_BIN:-/opt/fragmentor/fragmentor}
state=${FRAGMENTOR_STATE:-/var/lib/fragmentor}
backups=${FRAGMENTOR_BACKUPS:-/var/backups}
keep_backups=10
wait_up=30 # seconds to wait for "polling for updates"

fail() {
	echo "deploy: $*" >&2
	exit 1
}

stopped=
on_exit() {
	status=$?
	rm -f -- "$new" "$0"
	if [ "$status" -ne 0 ] && [ -n "$stopped" ] && ! systemctl is-active --quiet fragmentor; then
		echo "deploy: failed, starting the bot again" >&2
		systemctl start fragmentor || true
	fi
}
trap on_exit EXIT

[ "$(id -u)" -eq 0 ] || fail "run as root: fragmentor-vps in ~/.ssh/config needs User root"
[ -f "$new" ] || fail "$new not found: scp did not copy the binary"

echo "deploy: stopping the bot"
systemctl stop fragmentor
stopped=1

backup="$backups/fragmentor-$(date +%Y%m%d-%H%M%S)"
echo "deploy: database copy in $backup"
cp -a "$state" "$backup"
# The names sort by time; older copies than the last few go.
ls -1d "$backups"/fragmentor-* | sort | head -n -"$keep_backups" | xargs -r rm -rf --

if [ -f "$bin" ]; then
	cp -a "$bin" "$bin.prev"
fi
install -m 755 "$new" "$bin.new"
mv "$bin.new" "$bin"

since=$(date '+%Y-%m-%d %H:%M:%S')
echo "deploy: starting the bot"
systemctl start fragmentor

log=
i=0
while [ "$i" -lt "$wait_up" ]; do
	log=$(journalctl -u fragmentor --since "$since" --no-pager -o cat)
	case $log in
	*'msg="polling for updates"'*) break ;;
	esac
	sleep 1
	i=$((i + 1))
done
echo "deploy: the log since the start:"
journalctl -u fragmentor --since "$since" -n 20 --no-pager -o cat

case $log in
*"msg=starting version=$version "*) ;;
*) fail "the log has no 'starting version=$version': see section 8 of DEPLOY.md, rollback in section 5" ;;
esac
case $log in
*'msg="polling for updates"'*) ;;
*) fail "no 'polling for updates' in $wait_up s: see section 8 of DEPLOY.md, rollback in section 5" ;;
esac
echo "deploy: version $version is up, the old binary is $bin.prev, the database copy is $backup"
