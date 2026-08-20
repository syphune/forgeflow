# Backend rules

Use vertical modules under `internal/`. A handler decodes and maps; a service validates invariants; a repository owns persistence. Keep transaction boundaries explicit. Use `context.Context`, `slog`, typed application errors, and small consumer-owned interfaces.

The work-item transition service is the only status mutation path. The specification service is the only quality-gate implementation. Keep domain values independent of transport DTOs.
