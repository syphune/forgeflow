CREATE TABLE repository_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    commit_sha text NOT NULL CHECK (length(btrim(commit_sha)) BETWEEN 7 AND 128),
    ref_name text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('READY', 'FAILED')),
    file_count integer NOT NULL DEFAULT 0 CHECK (file_count >= 0),
    symbol_count integer NOT NULL DEFAULT 0 CHECK (symbol_count >= 0),
    skipped_count integer NOT NULL DEFAULT 0 CHECK (skipped_count >= 0),
    error_message text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, repository_id, commit_sha),
    UNIQUE (organization_id, project_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id) ON DELETE CASCADE
);

CREATE TABLE repository_files (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    path text NOT NULL,
    language text NOT NULL DEFAULT 'text',
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0 AND size_bytes <= 262144),
    content_hash text NOT NULL,
    content bytea NOT NULL CHECK (octet_length(content) <= 262144),
    PRIMARY KEY (organization_id, snapshot_id, path),
    FOREIGN KEY (organization_id, project_id, snapshot_id)
        REFERENCES repository_snapshots(organization_id, project_id, id) ON DELETE CASCADE
);

CREATE TABLE repository_symbols (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    path text NOT NULL,
    name text NOT NULL,
    qualified_name text NOT NULL,
    kind text NOT NULL,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    confidence text NOT NULL DEFAULT 'candidate',
    provenance text NOT NULL DEFAULT 'EXTRACTED',
    FOREIGN KEY (organization_id, project_id, snapshot_id)
        REFERENCES repository_snapshots(organization_id, project_id, id) ON DELETE CASCADE
);

CREATE TABLE repository_edges (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    from_symbol text NOT NULL,
    to_symbol text NOT NULL,
    kind text NOT NULL,
    confidence text NOT NULL DEFAULT 'candidate',
    provenance text NOT NULL DEFAULT 'EXTRACTED',
    FOREIGN KEY (organization_id, project_id, snapshot_id)
        REFERENCES repository_snapshots(organization_id, project_id, id) ON DELETE CASCADE
);

CREATE INDEX repository_snapshots_latest_idx
    ON repository_snapshots (organization_id, project_id, repository_id, finished_at DESC, id DESC);
CREATE INDEX repository_files_path_idx
    ON repository_files (organization_id, project_id, snapshot_id, path);
CREATE INDEX repository_symbols_name_idx
    ON repository_symbols (organization_id, project_id, snapshot_id, lower(name));
CREATE INDEX repository_edges_from_idx
    ON repository_edges (organization_id, project_id, snapshot_id, from_symbol);

CREATE TABLE knowledge_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    repository_id uuid,
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{0,95}$'),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 160),
    kind text NOT NULL CHECK (kind IN ('ARCHITECTURE', 'CONVENTIONS', 'TESTING', 'DOMAIN_RULES', 'KNOWN_ISSUES', 'MODULE')),
    current_provenance text NOT NULL CHECK (current_provenance IN ('MANUAL', 'EXTRACTED', 'AI_PROPOSED', 'HUMAN_VERIFIED')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, project_id, slug),
    UNIQUE (organization_id, project_id, id),
    FOREIGN KEY (organization_id, project_id) REFERENCES projects(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, repository_id) REFERENCES repositories(organization_id, id) ON DELETE SET NULL
);

CREATE TABLE knowledge_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    document_id uuid NOT NULL,
    revision_number integer NOT NULL CHECK (revision_number > 0),
    content text NOT NULL CHECK (octet_length(content) <= 524288),
    provenance text NOT NULL CHECK (provenance IN ('MANUAL', 'EXTRACTED', 'AI_PROPOSED', 'HUMAN_VERIFIED')),
    source_snapshot_id uuid,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_id, document_id, revision_number),
    FOREIGN KEY (organization_id, project_id, document_id)
        REFERENCES knowledge_documents(organization_id, project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, project_id, source_snapshot_id)
        REFERENCES repository_snapshots(organization_id, project_id, id) ON DELETE SET NULL
);

CREATE INDEX knowledge_documents_project_idx
    ON knowledge_documents (organization_id, project_id, kind, updated_at DESC);
CREATE INDEX knowledge_revisions_document_idx
    ON knowledge_revisions (organization_id, document_id, revision_number DESC);
