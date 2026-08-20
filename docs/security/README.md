# Security baseline

Tenant scope is mandatory at the application boundary. Repository and agent content is untrusted and never changes platform instructions. The server does not execute arbitrary repository commands. Webhook signatures use HMAC-SHA256 with constant-time comparison. Tokens are stored as hashes, secrets are filtered from child-process environments, and untrusted downloads are attachments.
