# ADR-006: DB-backed, auto-generated, self-syncing project config

Status: accepted

Config YAML lives as text in state.db project_configs, history in config_revisions. No projects/ folder. Generated automatically on first seen via detect package scanning run-defining files. Ownership per app via auto_apps_json hashes. Sync signature SHA-256 over run files, not git HEAD, named run_signature. Detect and sync on project load and every start. Edit via web Config page and apply_config; export/edit via CLI replace direct file editing. User-owned apps needing update produce pending proposals approved or dismissed by humans.
