CREATE TABLE IF NOT EXISTS mutation_dedupe (
	method TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	client_request_id TEXT NOT NULL,
	payload_fingerprint TEXT NOT NULL,
	response_json BLOB,
	error_text TEXT NOT NULL,
	completed_at_unix_ms INTEGER NOT NULL,
	expires_at_unix_ms INTEGER NOT NULL,
	PRIMARY KEY (method, resource_id, client_request_id)
);
