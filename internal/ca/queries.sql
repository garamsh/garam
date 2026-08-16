-- name: CreateCertificateAuthority :exec
INSERT INTO certificate_authorities (org_grn, certificate, sealed_key, not_after, created_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetCertificateAuthority :one
SELECT * FROM certificate_authorities WHERE org_grn = $1;
