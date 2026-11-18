CREATE TABLE user_tokens (
    token TEXT PRIMARY KEY,
    user_id VARCHAR(36) NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX user_tokens_user_id_idx ON user_tokens (user_id);
