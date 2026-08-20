# ADR-0001: Modular monolith

Status: accepted

Forgeflow starts as one Go process with vertical feature modules. This keeps transactions, debugging, and local development simple while preserving boundaries for later extraction. Microservices, CQRS, and event sourcing are deferred until measured operational evidence requires them.
