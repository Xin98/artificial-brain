# Private deployment (single host)

The same compose stack that runs the cloud form also runs the private form:
one administrator, one Docker Compose project, PostgreSQL included. Private
mode is a configuration switch (`DEPLOYMENT_MODE=private`), not a different
codebase.

## Quick start

1. Copy the template next to `compose.yaml` and fill it in:

   ```sh
   cp deploy/private/env.template .env
   ```

   Required edits:

   - `DEPLOYMENT_MODE=private` (already set in the template)
   - `PRIVATE_ADMIN_PHONE` and/or `PRIVATE_ADMIN_EMAIL` — the administrator's
     E.164 phone number and/or email address (at least one; when both are set
     they belong to the same admin user). These are the only identifiers that
     can ever log in, and the API refuses to start without at least one.
   - `POSTGRES_PASSWORD` and `REMINDER_RECEIPT_SECRET` — real random values.
   - Real model/SMTP configuration (`MODEL_*`, `SMTP_*`, `REMINDER_SMTP_*`);
     the fake adapters and dev inbox/outbox are forbidden under
     `APP_ENV=production`. SMS reminders stay `disabled` until an SMS
     provider is configured.

2. Build and start the stack:

   ```sh
   docker compose up -d --build
   ```

   The one-shot `migrate` service applies all migrations before `api` and
   `worker` start; on first boot the API provisions the fixed administrator
   from the configured administrator identifier(s) (`PRIVATE_ADMIN_PHONE`
   and/or `PRIVATE_ADMIN_EMAIL`).

3. Verify:

   ```sh
   curl http://127.0.0.1:8080/health/ready   # api
   curl http://127.0.0.1:3000/health/live    # web
   ```

   Then open `http://127.0.0.1:3000/`, log in with the administrator phone
   number or email address, and enter the verification code delivered through
   your configured SMS/SMTP adapter. Every other identifier receives
   `registration_closed`.

## No reverse proxy in the box

This deployment deliberately ships **no reverse proxy** (iteration decision
D8, deviation from master design §9.2):

- **Default is host-local binding.** The template publishes ports as
  `127.0.0.1:3000`/`127.0.0.1:8080`, so only the host itself can reach the
  stack.
- **LAN exposure risk.** If you rebind (`WEB_PORT=3000`) to open the stack
  to the LAN, every device on that LAN can reach the login page over plain
  HTTP — no TLS and no network-layer authentication. The single-admin
  login gate with SMS verification codes is then your only protection. Only
  do this on a trusted network, and prefer the enterprise gateway below.
- **Enterprise proxy hookup.** Point your enterprise gateway/reverse proxy
  at the web service only — forward to `web:3000` (host loopback port 3000
  when using the template, or add the gateway to the compose network). Let
  the gateway own TLS and enterprise authentication.
  - Health check for the gateway: `GET /health/live` (returns 200 while the
    web container is alive).
  - Only one origin is needed: the web service rewrites `/api/*` to the
    internal API, so browsers never talk to the API port directly.
  - Do not publish or forward the API port, PostgreSQL, or the worker.

## Backup and restore

Operator scripts wrap `pg_dump`/`pg_restore` with a CONFIRM gate:

```sh
make backup                                        # archive + sha256 sidecar
make restore BACKUP=<archive> CONFIRM=restore      # destructive, gated
```

Archives land in `deploy/private/backups/` (gitignored). Details, contents,
and restore semantics: [`docs/runbooks/backup-restore.md`](../../docs/runbooks/backup-restore.md).

## Upgrade

Back up first, then rebuild and let the one-shot migration run; the full
checklist (including the append-only migrations rule and the restore path on
failure) is in [`docs/runbooks/upgrade.md`](../../docs/runbooks/upgrade.md).

## Offline install

`make offline-bundle` saves every stack image
(`postgres:18.4-alpine` plus the built service images) to
`.artifacts/offline/artificial-brain-images.tar` with a `docker load`
recipe README. Artifacts are never committed.

## Cloud deployment

For the Alibaba Cloud ECS form of this private deployment (public-IP access
behind a locked-down security group, personal-mailbox SMTP, DashScope model),
follow [`docs/runbooks/cloud-ecs.md`](../../docs/runbooks/cloud-ecs.md).
