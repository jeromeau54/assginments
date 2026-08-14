# Fixes

This log records each fix made while completing the assessment tasks, in the format:

- **What was broken**
- **How I found it** (error messages, commands run)
- **What I changed**

---

## Tier 1 — Get it running

### 1. Pinger binary copied to wrong path (container fails to start)

- **Broken:** `services/pinger/Dockerfile` built the binary as `/go/src/pinger/app` and copied it into the runtime image at `/usr/local/bin/pinger`, but `ENTRYPOINT` pointed at `/app/pinger`. The path didn't exist, so the container would exit immediately with `exec: "/app/pinger": stat /app/pinger: no such file or directory`.
- **Found via:** Static read of the Dockerfile — `COPY --from=builder ... /usr/local/bin/pinger` vs `ENTRYPOINT ["/app/pinger"]`.
- **Fix:** Copy the binary to `/app/pinger` (matching the entrypoint) and, for parity with the gateway image, run as a non-root `appuser` (uid 1000), which also matches the `runAsUser: 1000` security context already set in the k8s pinger Deployment.

### 2. Gateway couldn't resolve the pinger container (`docker-compose.yml`)

- **Broken:** The compose service was named `pinger-svc`, but the gateway's `PINGER_HOST` env var (and the app's own default in `main.go`) is `pinger`. Docker Compose's embedded DNS only resolves by service name, so `gateway` would fail with a DNS lookup error the first time it called `/ping` (proxy to `http://pinger:8000/`).
- **Found via:** Cross-referencing `docker-compose.yml`'s service name against `services/gateway/cmd/gateway/main.go`'s `getEnv("PINGER_HOST", "pinger")` default and the `PINGER_HOST=pinger` value set in compose.
- **Fix:** Renamed the service from `pinger-svc` to `pinger` (and updated `depends_on` accordingly) so the DNS name matches what gateway actually looks up.

### 3. Redis only bound to loopback, unreachable from other containers

- **Broken:** `services/redis/redis.conf` had `bind 127.0.0.1`. Since each container has its own network namespace, this made Redis reachable only from inside its own container — gateway and pinger (in separate containers) could never connect, e.g. `redis.Ping()` failing with connection refused/timeout.
- **Found via:** Reading `redis.conf` against how compose networks containers (each service = separate container, reachable only via the service's own listening interfaces).
- **Fix:** Changed `bind` to `0.0.0.0` and `protected-mode` to `no` (there's no `requirepass` set, and protected-mode blocks all non-loopback connections without one). Redis is not published to the host and is only reachable on the internal `app-network`, so this is an acceptable internal-network trade-off — revisited in `docs/trade-offs.md` for Tier 3.

### 4. Redis `dir` didn't match its mounted volume (or exist at all)

- **Broken:** `redis.conf` set `dir /var/lib/redis`, but that directory doesn't exist in the `redis:7-alpine` image (which ships with `/data` as its working/volume directory) and wasn't created by anything in the compose file. Redis fails to start with `Can't chdir to '/var/lib/redis'`. Separately, the compose volume was mounted at `/data/redis`, which didn't match `dir` either way.
- **Found via:** Comparing `redis.conf`'s `dir` setting, the compose `volumes:` mount path, and the base image's default `/data` volume/workdir.
- **Fix:** Set `dir /data` in `redis.conf` and mounted the `redis-data` volume at `/data` in `docker-compose.yml`, so Redis starts cleanly and persists to the mounted volume.

### 5. No healthchecks, so "all 3 services healthy" was unverifiable and startup order was unenforced

- **Broken:** None of the three compose services had a `healthcheck`, and `depends_on` only waited for containers to *start*, not for redis/pinger to actually be ready. Gateway could start before Redis/pinger were accepting connections.
- **Found via:** Tier 1 goal explicitly requires "all 3 services healthy"; `docker compose ps` had no way to report that without healthchecks defined.
- **Fix:** Added a `healthcheck` to each service (`wget --spider /healthz` for gateway/pinger, `redis-cli ping` for redis) and changed `depends_on` to use `condition: service_healthy` so gateway waits on both pinger and redis, and pinger waits on redis.
