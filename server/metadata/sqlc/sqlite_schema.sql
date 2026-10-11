CREATE TABLE sqlite_schema (
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    sql TEXT
);

CREATE TABLE pragma_table_list (
    name TEXT NOT NULL,
    type TEXT NOT NULL
);
