CREATE TABLE Meta (key TEXT PRIMARY KEY, value TEXT);
INSERT INTO Meta VALUES ('databaseVersion', '<?xml version="1.0"?><plist><integer>24</integer></plist>');
CREATE TABLE TMTask (
    uuid TEXT PRIMARY KEY, type INTEGER NOT NULL DEFAULT 0,
    status INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '', trashed INTEGER NOT NULL DEFAULT 0,
    start INTEGER NOT NULL DEFAULT 0, startDate INTEGER,
    startBucket INTEGER NOT NULL DEFAULT 0, deadline INTEGER,
    deadlineSuppressionDate INTEGER, reminderTime INTEGER,
    "index" INTEGER NOT NULL DEFAULT 0, todayIndex INTEGER NOT NULL DEFAULT 0,
    area TEXT, project TEXT, heading TEXT, creationDate REAL,
    userModificationDate REAL, stopDate REAL, rt1_recurrenceRule BLOB,
    rt1_repeatingTemplate TEXT
);
CREATE TABLE TMArea (uuid TEXT PRIMARY KEY, title TEXT NOT NULL, "index" INTEGER DEFAULT 0);
CREATE TABLE TMTag (uuid TEXT PRIMARY KEY, title TEXT NOT NULL, "index" INTEGER DEFAULT 0);
CREATE TABLE TMTaskTag (tasks TEXT, tags TEXT);
CREATE TABLE TMAreaTag (areas TEXT, tags TEXT);
CREATE TABLE TMChecklistItem (
    uuid TEXT PRIMARY KEY, title TEXT NOT NULL, status INTEGER DEFAULT 0,
    task TEXT, "index" INTEGER DEFAULT 0
);
