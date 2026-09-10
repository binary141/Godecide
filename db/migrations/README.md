# migrations

SQL migration files and a small helper tool for creating them.

## Creating a migration

Run `db.go` from this directory to generate a new timestamped `.sql` file.

```bash
# No name — creates <unix_timestamp>.sql
go run db.go

# With a name — creates <unix_timestamp>_create_users.sql
go run db.go create users
```

The Unix timestamp prefix ensures migrations are applied in creation order.

## Applying migrations

Migrations are applied automatically on application startup. The runner (in `db/db.go`) tracks applied migrations in the `schema_migrations` table and skips any that have already been run.
