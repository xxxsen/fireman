# Repository Instructions

## Database Migrations

- `migrations/*.sql` may contain DDL only. DML statements such as `INSERT`,
  `UPDATE`, `DELETE`, `REPLACE`, data backfills, or data-copy statements are
  prohibited.
- This project has not been released. Do not add compatibility migrations for
  development data. Change the consolidated baseline schema directly and
  rebuild `.dev-data` databases when the schema changes.
- Until the first production release, keep the complete schema in the single
  `migrations/0001_init.sql` baseline. Do not add incremental SQL migration
  files.
- Application-owned reference or seed data must be initialized by explicit,
  idempotent Go bootstrap code outside `migrations/`.
- Every schema change must be verified by applying the baseline to an empty
  database, running `PRAGMA foreign_key_check`, and confirming migration SQL
  contains no DML.
