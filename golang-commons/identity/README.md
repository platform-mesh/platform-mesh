# OIDC principals

This package is not wired into authentication or authorization. Activation
requires coordinated migration of existing identities and authorization data.
It does not rewrite service accounts, administrators, or existing FGA users.

The username is `oidc:v1:<issuer code-point count>:<exact issuer><exact subject>`.
Issuer and subject are not normalized. The count makes the tuple unambiguous,
including when fields contain colons or Unicode. `UsernameExpression` produces
the same username in KCP CEL **for claims accepted by `Principal.Validate`**;
the mapping itself does not validate claims. `ClaimValidationExpression` checks
claim presence and types, nonempty values, controls, and the UTF-8 byte limit.
Use it together with the mapping. It does not validate issuer URL syntax or
trust: validate the configured issuer with `Principal.Validate` and ensure KCP
checks the token issuer for exact equality. Token decoding must reject invalid
UTF-8 independently.

Issuers must be absolute HTTPS URLs with a host and no userinfo, query, or
fragment. Both fields must be valid UTF-8 without control characters; subjects
must be nonempty. The encoded username is limited to 1024 bytes as a
conservative operational bound, not a guarantee against deployment-specific
HTTP, proxy, or storage limits.

`Principal.FGAUser` returns `user:oidc_v1_` followed by lowercase SHA-256 of
the UTF-8 canonical username. The hash is deterministic and not encryption:
it can be guessed for low-entropy inputs and cannot be decoded to recover a
principal. Neither the username nor its hash authenticates the identity or
detects tampering; a changed length may describe a different valid principal.
Callers must authenticate claims and trust the issuer independently.
