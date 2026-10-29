-- Telegram users of the bot. The first /start creates the user and puts them
-- on the beta waitlist. language is the bot language, 'ru' or 'en'.
-- Times are unix seconds, UTC.
CREATE TABLE users (
    telegram_id        INTEGER PRIMARY KEY,
    username           TEXT,
    language           TEXT    NOT NULL,
    joined_waitlist_at INTEGER,
    created_at         INTEGER NOT NULL
) STRICT;
