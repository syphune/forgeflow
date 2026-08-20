# ADR-0002: PostgreSQL and transactional outbox

Status: accepted

PostgreSQL is the source of truth. Important mutations append audit and outbox records transactionally, and workers process events at least once with idempotency keys. Redis and a dedicated broker are deferred until measured throughput or latency requires them.
