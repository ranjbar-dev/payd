-- RL-006 / DB-002a: retain UTC-day request totals for the seven-day quota trend.
CREATE TABLE trongrid_daily_requests (
    day_start INTEGER PRIMARY KEY,
    requests  INTEGER NOT NULL CHECK (requests >= 0),
    updated_at INTEGER NOT NULL
);
