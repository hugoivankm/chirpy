-- name: CreateChirp :one
INSERT INTO chirps (created_at, updated_at, body, user_id)
VALUES (
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: DeleteChirps :exec
DELETE FROM chirps;