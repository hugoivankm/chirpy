-- +goose UP
CREATE TABLE Users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    email TEXT UNIQUE NOT NULL
);

-- +goose Down
DROP TABLE Users;