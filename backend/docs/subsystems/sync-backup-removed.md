# Removed synchronization and portable backups

Cross-computer WebDAV/S3/GitHub synchronization, portable configuration bundles,
automatic backups, and their GUI/API/CLI surfaces have been removed from this
product. There is no synchronization worker or scheduled backup worker at
gateway startup. Legacy files stay on disk and cannot enable these features.

The implementation packages `internal/backup` and `internal/davsync`, their
management routes and commands, portable library encoding and restoration,
provider mirroring, and sign-in credential transport are deleted. Dedicated
quota-history merges, usage transfer writers and machine-local restore overlays
are also removed. Request archives are local; no sync-configured S3 bucket is
used. Existing historical shared-usage files have read-only compatibility.

Client configuration transactions retain atomic writes, original-file copies,
failure rollback, and disconnect recovery. Library changes still protect the
client files they modify. Applying a local library to local clients remains an
independent operation. Historical JSON fields may remain for data compatibility.

See [the product removal record](../../../docs/sync-backup-removal.md) and
the regressions in `internal/gui/uni_switch_sync_backup_test.go`,
`internal/cli/uni_switch_sync_backup_test.go`, and
`scripts/qa-source-sync-backup.mjs` at the repository root.
