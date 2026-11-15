-- The Codex app's catalog tables in <CODEX_HOME>/sqlite/codex.db, as its migrations made them (codex_schema_migrations up to legacy_0034, app 26.901 to 26.1023): sqlite3 .schema
CREATE TABLE IF NOT EXISTS "local_thread_catalog" (
            host_id TEXT NOT NULL,
            thread_id TEXT NOT NULL,
            display_title TEXT NOT NULL,
            source_created_at REAL NOT NULL,
            source_updated_at REAL NOT NULL,
            cwd TEXT,
            source_kind TEXT NOT NULL,
            source_detail TEXT,
            model_provider TEXT,
            git_branch TEXT,
            observation_sequence INTEGER NOT NULL,
            missing_candidate INTEGER NOT NULL DEFAULT 0
              CHECK (missing_candidate IN (0, 1)),
            thread_source TEXT,
            source_recency_at REAL NOT NULL DEFAULT 0,
            pending_observed_title INTEGER NOT NULL DEFAULT 0,
            project_id TEXT,
            conversation_origin TEXT, trial_conversation_type TEXT, chatgpt_async_status INTEGER,
            PRIMARY KEY (host_id, thread_id)
          );
CREATE INDEX local_thread_catalog_updated_idx
            ON local_thread_catalog (
              host_id, source_recency_at DESC, source_created_at DESC, thread_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_created_idx
            ON local_thread_catalog (
              host_id, source_created_at DESC, source_updated_at DESC, thread_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_cwd_updated_idx
            ON local_thread_catalog (
              host_id, cwd, source_recency_at DESC, source_created_at DESC,
              thread_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_cwd_created_idx
            ON local_thread_catalog (
              host_id, cwd, source_created_at DESC, source_updated_at DESC,
              thread_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_thread_lookup_idx
            ON local_thread_catalog (
              thread_id, source_recency_at DESC, source_created_at DESC, host_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_project_updated_idx
            ON local_thread_catalog (
              host_id, project_id, source_recency_at DESC,
              source_created_at DESC, thread_id
            ) WHERE missing_candidate = 0;
CREATE INDEX local_thread_catalog_origin_updated_idx
            ON local_thread_catalog (
              host_id, conversation_origin, source_recency_at DESC,
              source_created_at DESC, thread_id
            ) WHERE missing_candidate = 0;
CREATE TABLE IF NOT EXISTS "local_thread_catalog_hosts" (
            host_id TEXT PRIMARY KEY,
            host_kind TEXT NOT NULL CHECK (
              host_kind IN ('local', 'ssh', 'wsl', 'remote-control', 'chatgpt')
            )
          );
CREATE TABLE local_thread_catalog_metadata (
            id INTEGER PRIMARY KEY CHECK (id = 1),
            catalog_revision INTEGER NOT NULL DEFAULT 0
          );
CREATE TABLE local_thread_catalog_sync_state (
            host_id TEXT PRIMARY KEY,
            watermark_updated_at REAL,
            initial_build_complete INTEGER NOT NULL DEFAULT 0,
            observation_sequence INTEGER NOT NULL DEFAULT 0
          , last_full_reconciled_at INTEGER);
CREATE TABLE local_thread_catalog_scan_entries (
            host_id TEXT NOT NULL,
            thread_id TEXT NOT NULL,
            removed INTEGER NOT NULL DEFAULT 0,
            PRIMARY KEY (host_id, thread_id)
          ) WITHOUT ROWID;
