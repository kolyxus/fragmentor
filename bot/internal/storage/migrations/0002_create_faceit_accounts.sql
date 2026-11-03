-- FACEIT accounts linked with /link, one per Telegram user: linking another
-- account replaces it. Several users may link the same FACEIT account: the
-- bot does not check who owns it, and a unique player_id would let anyone
-- lock the owner out. Deleting the user deletes the link.
-- player_id is the FACEIT player ID (a UUID), steam_id64 the SteamID64 of the
-- CS2 profile, nickname the FACEIT nickname at the time of linking.
-- Times are unix seconds, UTC.
CREATE TABLE faceit_accounts (
    telegram_id INTEGER PRIMARY KEY REFERENCES users (telegram_id) ON DELETE CASCADE,
    player_id   TEXT    NOT NULL,
    nickname    TEXT    NOT NULL,
    steam_id64  TEXT    NOT NULL,
    linked_at   INTEGER NOT NULL
) STRICT;
