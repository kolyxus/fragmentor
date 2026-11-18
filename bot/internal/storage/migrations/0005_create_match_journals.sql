-- Step 8a: the journal of every parsed match, every player of it with the
-- metrics, and the facts of the reviews of the users of the bot
-- (docs/DECISIONS.md, 2026-11-09, the storage). The demo itself is never
-- kept. Times are unix seconds, UTC.
--
-- The anonymous reference samples of step 7 go: the norms of each FACEIT
-- level (step 11) come from the metrics of all the players of the matches.
-- The matches parsed before have no journal and their metrics cannot be
-- counted again, so they go too: a fresh link parses such a match again.
DROP TABLE reference_samples;
DROP TABLE match_players;
DELETE FROM matches;

-- match_journals: the journal of a parsed match, a row a match: the summary
-- and the events of the demo, the JSON of demo.Match compressed with zstd,
-- with the players as their seats and no names (demo.Anonymize). version is
-- demo.JournalVersion: a journal of an older one needs the demo again.
CREATE TABLE match_journals (
    match_id   TEXT    PRIMARY KEY REFERENCES matches (match_id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    journal    BLOB    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

-- match_players: every player of a parsed match. seat is his number in the
-- journal, steam_id64 who he is; player_id and skill_level, the FACEIT level
-- in the match, come from the roster of the match, NULL if it lacks him;
-- team 0 or 1 as in matches. metrics is JSON of the metrics for the match and
-- for each side, of the version in matches. facts is JSON of the facts of
-- his review (findings.Facts), kept for a user of the bot only, that is, a
-- player whose account some user has linked; facts_version is their
-- findings.FactsVersion.
CREATE TABLE match_players (
    match_id      TEXT    NOT NULL REFERENCES matches (match_id) ON DELETE CASCADE,
    seat          INTEGER NOT NULL CHECK (seat >= 1),
    steam_id64    TEXT    NOT NULL,
    player_id     TEXT,
    skill_level   INTEGER,
    team          INTEGER NOT NULL CHECK (team IN (0, 1)),
    metrics       TEXT    NOT NULL,
    facts         TEXT,
    facts_version INTEGER,
    PRIMARY KEY (match_id, seat),
    CHECK ((facts IS NULL) = (facts_version IS NULL))
) STRICT;

-- A user of the bot is found in the matches by his SteamID64.
CREATE INDEX match_players_by_steam_id ON match_players (steam_id64);

-- The matches in the order they were played: the past of a player.
CREATE INDEX matches_by_start ON matches (started_at);
