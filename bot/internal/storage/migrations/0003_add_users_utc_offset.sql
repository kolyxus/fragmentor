-- The time on the user's clock as minutes east of UTC, chosen with the time
-- zone button under /matches. NULL means the default of the bot language:
-- Moscow time for Russian, UTC for English.
ALTER TABLE users ADD COLUMN utc_offset INTEGER;
