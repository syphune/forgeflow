# Desktop rules

Keep repository, process, OAuth, and credential access in the main process. Preload exposes a small typed API; renderer code never receives generic IPC or Node access. Validate paths against an app-managed root, use fixed executable/argument construction, redact output, and require explicit human approval before code-changing runs. Desktop AgentRun sync may use a short-lived PAT or an OS-encrypted session, but credentials must never enter the provider environment or command arguments.
