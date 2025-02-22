-- name: CreateUser :one
INSERT INTO users (email)
VALUES ("nishanth@gmail.com")
RETURNING *;