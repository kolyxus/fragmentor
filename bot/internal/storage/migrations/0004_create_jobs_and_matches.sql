-- Step 7: the demos users send links to, and what is kept of them: the
-- match, the metrics of the players who use the bot, and the anonymous
-- samples of every player for the norms of each FACEIT level. The demo
-- itself is never kept. Times are unix seconds, UTC.

-- jobs: a demo link a user sent and what became of it. A job waits in the
-- queue ('queued'), is parsed by a worker ('running') and ends 'done' or
-- 'failed' with the reason. demo_url is the signed link, a credential for a
-- few minutes: it is wiped when the job ends. link_expires_at is when the
-- link runs out, NULL if it does not tell. attempts counts how many times a
-- worker took the job, so that a demo the bot died on is not tried for ever.
-- status_message_id is the message the bot keeps the status in, NULL until
-- it is sent. Jobs stay after they end.
CREATE TABLE jobs (
    id                INTEGER PRIMARY KEY,
    telegram_id       INTEGER NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    match_id          TEXT    NOT NULL,
    demo_url          TEXT,
    link_expires_at   INTEGER,
    language          TEXT    NOT NULL,
    status            TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    reason            TEXT,
    status_message_id INTEGER,
    attempts          INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    started_at        INTEGER,
    finished_at       INTEGER
) STRICT;

-- The queue: the jobs of each status in order.
CREATE INDEX jobs_by_status ON jobs (status, id);

-- A user has one job at a time for a match.
CREATE UNIQUE INDEX jobs_one_active ON jobs (telegram_id, match_id) WHERE status IN ('queued', 'running');

-- matches: the FACEIT matches whose demo was parsed, a row each. Team 0 is
-- the one that started the match as CT, as in demo.Match. metrics_version is
-- the version of the formulas (metrics.Version) the match was parsed with:
-- a match adds its reference samples once per version.
CREATE TABLE matches (
    match_id        TEXT    PRIMARY KEY,
    map             TEXT    NOT NULL,
    started_at      INTEGER,
    team0_score     INTEGER NOT NULL,
    team1_score     INTEGER NOT NULL,
    rounds          INTEGER NOT NULL,
    suspicious      INTEGER NOT NULL CHECK (suspicious IN (0, 1)),
    warnings        TEXT,
    metrics_version INTEGER NOT NULL,
    parsed_at       INTEGER NOT NULL
) STRICT;

-- match_players: the players of a parsed match who use the bot, that is,
-- whose FACEIT account some user had linked when the match was parsed.
-- Nobody else is kept with an ID. skill_level is the FACEIT level in the
-- match, team 0 or 1 as in matches, metrics JSON of the metrics for the
-- match and for each side, without IDs.
CREATE TABLE match_players (
    match_id    TEXT    NOT NULL REFERENCES matches (match_id) ON DELETE CASCADE,
    player_id   TEXT    NOT NULL,
    steam_id64  TEXT    NOT NULL,
    skill_level INTEGER,
    team        INTEGER NOT NULL CHECK (team IN (0, 1)),
    metrics     TEXT    NOT NULL,
    PRIMARY KEY (match_id, player_id)
) STRICT;

CREATE INDEX match_players_by_player ON match_players (player_id);

-- reference_samples: the metrics of every player of the parsed matches, for
-- the norms of each FACEIT level (step 11), with nothing to tell whose they
-- are or which match they come from: no IDs, no match ID, no time. The ids
-- are random, so that neither they nor the order of the rows tell when a
-- sample came. A match with a suspicious summary adds none, nor does a
-- player of an unknown level.
CREATE TABLE reference_samples (
    id              INTEGER PRIMARY KEY,
    skill_level     INTEGER NOT NULL CHECK (skill_level BETWEEN 1 AND 10),
    map             TEXT    NOT NULL,
    metrics_version INTEGER NOT NULL,
    metrics         TEXT    NOT NULL
) STRICT;

CREATE INDEX reference_samples_by_level ON reference_samples (metrics_version, skill_level);
