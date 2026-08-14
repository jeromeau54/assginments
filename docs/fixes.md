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

### 6. Redis was never deployed to Kubernetes at all

- **Broken:** `k8s/base/kustomization.yaml` only listed `namespace.yaml`, `gateway/`, and `pinger/` as resources. `k8s/base/redis/` (with its own valid `kustomization.yaml` covering deployment/service/pvc/configmap) existed but was never referenced, so `kubectl apply -k k8s/base/` (the `make k8s-deploy` target) silently never created any redis objects.
- **Found via:** Comparing `k8s/base/kustomization.yaml`'s `resources:` list against the actual `k8s/base/*` directories.
- **Fix:** Added `redis/` to the base kustomization's resource list.

### 7. Gateway pod listened on the wrong port for its own probes/service

- **Broken:** `k8s/base/gateway/deployment.yaml` declared `containerPort: 8080` and pointed `livenessProbe`/`readinessProbe` at port `8080`, but the gateway binary actually listens on `8000` (`PORT: "8000"` from `gateway-config`, and `main.go` binds to `PORT`). Probes would fail with connection refused, so the pod would never become Ready.
- **Found via:** Comparing the deployment's port/probe values against `gateway-config`'s `PORT` and `main.go`'s listen address.
- **Fix:** Changed `containerPort` and both probes to `8000`.

### 8. Gateway Service had no matching pods

- **Broken:** `k8s/base/gateway/service.yaml`'s selector was `app: gw`, but the Deployment's pod template label is `app: gateway`. The Service would have zero Endpoints — nothing could ever reach gateway through it.
- **Found via:** Comparing `service.yaml`'s `selector` against `deployment.yaml`'s `template.metadata.labels`.
- **Fix:** Changed the selector to `app: gateway`.

### 9. Gateway/pinger pointed at each other's pod port instead of their Service port

- **Broken:** `gateway-config` set `PINGER_PORT: "8000"` and `pinger-cm` set `TARGET_PORT: "8000"`, but both the gateway and pinger Services expose port `80` (mapping to the pod's `8000` via `targetPort`). Traffic sent to `pinger:8000` or `gateway:8000` (the Service names) has nothing listening on `8000` — only the Service's own port `80` is routable — so gateway's `/ping` proxy and pinger's background pinger would both fail to connect.
- **Found via:** Comparing each ConfigMap's target port against the corresponding Service's `port:`/`targetPort:`.
- **Fix:** Changed `PINGER_PORT` and `TARGET_PORT` to `"80"` to match the Service ports.

### 10. Pinger's target host pointed at the wrong namespace

- **Broken:** `pinger-cm` set `TARGET_HOST: "gateway.default.svc.cluster.local"`, but `k8s/base/kustomization.yaml` deploys everything into the `assessment` namespace, not `default`. DNS lookup would fail, so the background pinger would never reach gateway, `target_up` would stay `false` forever, and `/readyz` (and therefore the pod's readiness) would never pass — meaning the pinger Service would never get Endpoints.
- **Found via:** Comparing `TARGET_HOST`'s namespace suffix against `namespace: assessment` in `k8s/base/kustomization.yaml`.
- **Fix:** Changed `TARGET_HOST` to `gateway.assessment.svc.cluster.local`.

### 11. Redis Service exposed the wrong port

- **Broken:** `k8s/base/redis/service.yaml` used `port: 6380` / `targetPort: 6380`, but Redis actually listens on `6379` (`redis.conf`'s `port 6379`, and the Deployment's `containerPort: 6379`). Traffic to the Service's port 6380 had nothing to forward to, and gateway/pinger's `REDIS_PORT: "6379"` didn't match the Service port either way.
- **Found via:** Comparing `service.yaml` against `redis-config`'s `redis.conf` and the Deployment's `containerPort`.
- **Fix:** Changed both `port` and `targetPort` to `6379`.

### 12. Redis PVC was declared but never mounted

- **Broken:** `k8s/base/redis/deployment.yaml` defined a `redis-data` volume backed by the `redis-data` PVC, but only mounted the `redis-config` volume — `redis-data` was never mounted into the container, so Redis wrote to the container's ephemeral filesystem instead of persistent storage.
- **Found via:** Reading the Deployment's `volumes:` list against its `volumeMounts:`.
- **Fix:** Added a `volumeMounts` entry mounting `redis-data` at `/data` (matching `redis.conf`'s `dir /data`).

### 13. Redis PVC requested a storage class that doesn't exist on a local cluster

- **Broken:** `k8s/base/redis/pvc.yaml` set `storageClassName: gp2`, an AWS EBS class. On a local `kind` cluster (or most non-AWS clusters) no `gp2` provisioner exists, so the PVC would stay `Pending` forever and the redis pod would hang in `ContainerCreating`, failing Tier 1's "all pods Running" goal.
- **Found via:** Recognizing `gp2` as an AWS-specific StorageClass name while the README's prerequisites target `kind` for local development.
- **Fix:** Removed `storageClassName` so the PVC uses the cluster's default StorageClass (`standard` on `kind`, and portable to any other cluster with a default class configured).
