# Runbook: upgrade

Upgrades are same-architecture and in-place: new images plus append-only
migrations, applied by the one-shot `migrate` service. There is no
rolling-upgrade path for the single-host private form — schedule a short
maintenance window.

## Checklist

1. **Verify the current version is healthy.**

   ```sh
   docker compose ps
   curl http://127.0.0.1:8080/health/ready
   curl http://127.0.0.1:3000/health/live
   ```

   Confirm all services are up and note the running version
   (`SERVICE_VERSION` in `.env` / image tags).

2. **Back up.** Never skip this step.

   ```sh
   make backup
   ```

   Confirm the printed archive path and its `.sha256` sidecar exist
   (see `docs/runbooks/backup-restore.md`).

3. **Replace images/refs.**
   - Online host: update the image references/pins you are upgrading
     (base images in the Dockerfiles, `postgres` tag in `compose.yaml`).
   - Offline host: build the new bundle on a connected machine
     (`make offline-bundle`), transfer
     `.artifacts/offline/artificial-brain-images.tar`, and
     `docker load --input artificial-brain-images.tar`.

4. **Recreate the stack and let migrate run.**

   ```sh
   docker compose up -d --build
   ```

   `migrate` is a one-shot service gated by `service_completed_successfully`;
   `api` and `worker` only start after all pending migrations have been
   applied. Wait for `migrate` to exit 0:

   ```sh
   docker compose ps --all
   ```

5. **Verify health and spot-check data.**

   ```sh
   curl http://127.0.0.1:8080/health/ready
   curl http://127.0.0.1:3000/health/live
   ```

   Then open the web UI, log in as the administrator, and spot-check that
   pre-upgrade data survived: existing todos, reminder history, and the
   dashboard counters.

## Migrations are append-only

Files under `deploy/migrations/` that shipped in any environment are
read-only history: never edit, rename, or reorder them. Schema changes are
new migration files appended after the highest existing number. If a
migration turns out to be wrong, fix it forward with another migration.

## On failure

If the upgraded stack is unhealthy (migrate exits non-zero, health checks
fail, or data looks wrong):

1. Stop trying new images; keep the database as-is.
2. Restore the backup taken in step 2:

   ```sh
   make restore BACKUP=<archive-from-step-2> CONFIRM=restore
   ```

3. Revert the image references to the previous version and run
   `docker compose up -d --build` again.
4. Verify health and data before reopening the stack, and report the failed
   migration so it can be fixed forward.
