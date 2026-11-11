-- NTOJ schema, version 1.
-- Submission states (smallint) follow the historical NTOJ numbering:
--   1 AC, 2 PC, 3 WA, 4 RE, 5 RE(SIG), 6 TLE, 7 MLE, 8 OLE, 9 CE, 10 CLE,
--   11 IE, 12 JE, 100 judging, 101 pending, 102 skipped, 103 rejected.

CREATE TABLE accounts (
    id            serial PRIMARY KEY,
    email         text        NOT NULL CHECK (char_length(email) BETWEEN 3 AND 254),
    name          text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 32),
    password_hash text        NOT NULL,
    role          text        NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    disabled      boolean     NOT NULL DEFAULT false,
    photo_url     text        NOT NULL DEFAULT '',
    cover_url     text        NOT NULL DEFAULT '',
    motto         text        NOT NULL DEFAULT '' CHECK (char_length(motto) <= 100),
    last_ip       text        NOT NULL DEFAULT '',
    allowed_ip    text        NOT NULL DEFAULT '',
    last_language text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX accounts_email_key ON accounts (lower(email));

CREATE TABLE sessions (
    id           bigserial PRIMARY KEY,
    account_id   integer     NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    ip           text        NOT NULL DEFAULT '',
    user_agent   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_account_idx ON sessions (account_id);

CREATE TABLE problems (
    id              serial PRIMARY KEY,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    status          text        NOT NULL DEFAULT 'hidden' CHECK (status IN ('online', 'contest', 'hidden')),
    allow_submit    boolean     NOT NULL DEFAULT true,
    tags            text[]      NOT NULL DEFAULT '{}',
    statement       text        NOT NULL DEFAULT '',
    time_limit_ms   integer     NOT NULL DEFAULT 1000 CHECK (time_limit_ms BETWEEN 1 AND 600000),
    memory_limit_kb integer     NOT NULL DEFAULT 262144 CHECK (memory_limit_kb BETWEEN 1024 AND 16777216),
    checker         text        NOT NULL DEFAULT 'diff'
        CHECK (checker IN ('diff', 'diff-strict', 'float-4', 'float-6', 'float-9', 'testlib')),
    score_precision integer     NOT NULL DEFAULT 0 CHECK (score_precision BETWEEN 0 AND 3),
    languages       text[]      NOT NULL DEFAULT '{}', -- empty: every supported language
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE testcases (
    id          serial PRIMARY KEY,
    problem_id  integer NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
    name        text    NOT NULL,
    input_size  bigint  NOT NULL DEFAULT 0,
    output_size bigint  NOT NULL DEFAULT 0,
    UNIQUE (problem_id, name)
);

CREATE TABLE subtasks (
    problem_id   integer       NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
    idx          integer       NOT NULL CHECK (idx >= 1),
    score        numeric(10,3) NOT NULL CHECK (score >= 0),
    testcase_ids integer[]     NOT NULL DEFAULT '{}',
    depends      integer[]     NOT NULL DEFAULT '{}',
    PRIMARY KEY (problem_id, idx)
);

CREATE TABLE contests (
    id                  serial PRIMARY KEY,
    name                text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    creator_id          integer     REFERENCES accounts (id) ON DELETE SET NULL,
    desc_before         text        NOT NULL DEFAULT '',
    desc_during         text        NOT NULL DEFAULT '',
    desc_after          text        NOT NULL DEFAULT '',
    mode                text        NOT NULL DEFAULT 'ioi' CHECK (mode IN ('ioi', 'acm')),
    start_at            timestamptz NOT NULL,
    end_at              timestamptz NOT NULL,
    time_mode           text        NOT NULL DEFAULT 'fixed' CHECK (time_mode IN ('fixed', 'flexible')),
    duration_sec        integer     NOT NULL DEFAULT 0 CHECK (duration_sec >= 0),
    reg_mode            text        NOT NULL DEFAULT 'invited' CHECK (reg_mode IN ('invited', 'free', 'approval')),
    reg_end_at          timestamptz NOT NULL,
    languages           text[]      NOT NULL DEFAULT '{}',
    public_scoreboard   boolean     NOT NULL DEFAULT false,
    allow_other_pages   boolean     NOT NULL DEFAULT true,
    hide_admin          boolean     NOT NULL DEFAULT true,
    cooldown_sec        integer     NOT NULL DEFAULT 30 CHECK (cooldown_sec BETWEEN 0 AND 86400),
    freeze_minutes      integer     NOT NULL DEFAULT 0 CHECK (freeze_minutes >= 0),
    keep_frozen         boolean     NOT NULL DEFAULT false,
    penalty_minutes     integer     NOT NULL DEFAULT 20 CHECK (penalty_minutes >= 0),
    created_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at > start_at)
);

CREATE TABLE contest_problems (
    contest_id   integer NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
    problem_id   integer NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
    ord          integer NOT NULL,
    score_type   text    NOT NULL DEFAULT 'best' CHECK (score_type IN ('best', 'subtask_union')),
    result_style text    NOT NULL DEFAULT 'full' CHECK (result_style IN ('full', 'subtask', 'total')),
    PRIMARY KEY (contest_id, problem_id)
);
CREATE INDEX contest_problems_problem_idx ON contest_problems (problem_id);

CREATE TABLE contest_users (
    contest_id    integer     NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
    account_id    integer     NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    status        text        NOT NULL CHECK (status IN ('requested', 'approved', 'rejected', 'admin')),
    session_start timestamptz, -- flexible-time contests: the account's own window
    session_end   timestamptz,
    read_at       timestamptz NOT NULL DEFAULT now(), -- announcements / replies seen up to here
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contest_id, account_id)
);
CREATE INDEX contest_users_account_idx ON contest_users (account_id);

CREATE TABLE contest_announcements (
    id         serial PRIMARY KEY,
    contest_id integer     NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
    author_id  integer     REFERENCES accounts (id) ON DELETE SET NULL,
    subject    text        NOT NULL,
    content    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contest_announcements_contest_idx ON contest_announcements (contest_id);

CREATE TABLE contest_questions (
    id         serial PRIMARY KEY,
    contest_id integer     NOT NULL REFERENCES contests (id) ON DELETE CASCADE,
    asker_id   integer     NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    subject    text        NOT NULL,
    content    text        NOT NULL,
    reply      text,
    replier_id integer     REFERENCES accounts (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    replied_at timestamptz
);
CREATE INDEX contest_questions_contest_idx ON contest_questions (contest_id);

CREATE TABLE submissions (
    id         bigserial PRIMARY KEY,
    problem_id integer       NOT NULL REFERENCES problems (id) ON DELETE CASCADE,
    account_id integer       NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    contest_id integer       REFERENCES contests (id) ON DELETE CASCADE,
    language   text          NOT NULL,
    code       text          NOT NULL,
    created_at timestamptz   NOT NULL DEFAULT now(),
    state      smallint      NOT NULL DEFAULT 101,
    score      numeric(12,3) NOT NULL DEFAULT 0,
    time_ms    bigint        NOT NULL DEFAULT 0,
    memory_kb  bigint        NOT NULL DEFAULT 0,
    message    text          NOT NULL DEFAULT '',
    result     jsonb         NOT NULL DEFAULT '{"subtasks": [], "testcases": []}',
    priority   integer       NOT NULL DEFAULT 0,
    claimed_at timestamptz,
    judged_at  timestamptz
);
CREATE INDEX submissions_account_idx ON submissions (account_id, id DESC);
CREATE INDEX submissions_problem_idx ON submissions (problem_id, id DESC);
CREATE INDEX submissions_contest_idx ON submissions (contest_id, id DESC);
CREATE INDEX submissions_queue_idx ON submissions (priority DESC, id) WHERE state = 101;

CREATE TABLE bulletins (
    id         serial PRIMARY KEY,
    title      text        NOT NULL,
    content    text        NOT NULL,
    color      text        NOT NULL DEFAULT '',
    pinned     boolean     NOT NULL DEFAULT false,
    author_id  integer     REFERENCES accounts (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE boards (
    id          serial PRIMARY KEY,
    name        text        NOT NULL,
    visible     boolean     NOT NULL DEFAULT true,
    start_at    timestamptz NOT NULL,
    end_at      timestamptz NOT NULL,
    problem_ids integer[]   NOT NULL DEFAULT '{}',
    account_ids integer[]   NOT NULL DEFAULT '{}'
);

CREATE TABLE problem_classes (
    id          serial PRIMARY KEY,
    name        text      NOT NULL,
    description text      NOT NULL DEFAULT '',
    problem_ids integer[] NOT NULL DEFAULT '{}',
    owner_id    integer   REFERENCES accounts (id) ON DELETE CASCADE, -- NULL: official
    public      boolean   NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE class_collections (
    account_id integer NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    class_id   integer NOT NULL REFERENCES problem_classes (id) ON DELETE CASCADE,
    PRIMARY KEY (account_id, class_id)
);

CREATE TABLE questions (
    id         serial PRIMARY KEY,
    account_id integer     NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    content    text        NOT NULL,
    reply      text,
    replier_id integer     REFERENCES accounts (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    replied_at timestamptz,
    reply_seen boolean     NOT NULL DEFAULT false
);
CREATE INDEX questions_account_idx ON questions (account_id);

CREATE TABLE audit_logs (
    id          bigserial PRIMARY KEY,
    type        text        NOT NULL,
    message     text        NOT NULL,
    params      jsonb       NOT NULL DEFAULT '{}',
    operator_id integer     REFERENCES accounts (id) ON DELETE SET NULL,
    operator_ip text        NOT NULL DEFAULT '',
    contest_id  integer     REFERENCES contests (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_type_idx ON audit_logs (type, id DESC);
CREATE INDEX audit_logs_contest_idx ON audit_logs (contest_id, id DESC);
