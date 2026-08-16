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

### 14. Gateway/pinger pods stuck in `ErrImagePull` on kind

- **Broken:** `k8s/base/gateway/deployment.yaml` and `k8s/base/pinger/deployment.yaml` didn't set `imagePullPolicy`, so Kubernetes used its default for a `:latest` tag, which is `Always`. Even after `kind load docker-image devops/gateway:latest ...` had already put the image on every node, the kubelet still tried to pull `devops/gateway:latest` / `devops/pinger:latest` from Docker Hub (there's no such repo, and no registry credentials configured) and failed permanently, leaving both pods in `ImagePullBackOff` and never Running.
- **Found via:** `kubectl -n assessment get pods` showing `ErrImagePull`/`ImagePullBackOff`; `kubectl -n assessment describe pod -l app=gateway` showed `Failed to pull image "devops/gateway:latest": ... pull access denied, repository does not exist`.
- **Fix:** Added `imagePullPolicy: IfNotPresent` to the `gateway` and `pinger` containers so the kubelet uses the image already loaded by `kind load docker-image` instead of trying to pull it.

### 15. Pinger deployment referenced a ConfigMap that doesn't exist

- **Broken:** `k8s/base/pinger/deployment.yaml`'s `envFrom` pointed at `configMapRef: name: pinger-config`, but the actual object defined in `k8s/base/pinger/configmap.yaml` is named `pinger-cm`. `pinger-config` doesn't exist anywhere in the base, so the pod would fail to start with `CreateContainerConfigError: configmap "pinger-config" not found`.
- **Found via:** `kubectl apply -k k8s/base/` output listed `configmap/pinger-cm created`; cross-checking that name against `envFrom` in the pinger Deployment showed the mismatch (only surfaced once fix #14 above let the pod get far enough to hit this next).
- **Fix:** Changed `envFrom.configMapRef.name` to `pinger-cm`.

### Verification (Tier 1 goals)

- `go build` + `go test ./...` pass for both `services/gateway` and `services/pinger`.
- `docker build` succeeds for both images; `docker compose up -d --build` brings up all 3 containers and `docker compose ps` reports all three as `(healthy)`; `curl localhost:8000/healthz`, `/ping`, and `/readyz` all return successfully (`/ping` proxies through to pinger).
- `kind load docker-image` + `kubectl apply -k k8s/base/` brings up `gateway`, `pinger`, and `redis` Deployments, all pods `1/1 Running`, all three Services have non-empty Endpoints, and `kubectl -n assessment port-forward svc/gateway 18080:80` + `curl` confirms `/healthz`, `/ping`, `/readyz` all respond correctly through the cluster.

---

## Tier 2 — Make it reliable

CI fixes were verified against a real pipeline run, not just static review: stood up a local self-hosted GitLab CE instance + a Docker-executor `gitlab-runner` (both in Docker, on the same Docker network), pushed this repo to a local-only test project, and watched the pipeline actually execute. That test project/runner is local-machine infrastructure only — separate from both the GitHub mirror and the real graded assessment repo, neither of which were touched.

### 16. `test-gateway`/`test-pinger` had swapped `needs:`

- **Broken:** `test-gateway` declared `needs: [build-pinger]` and `test-pinger` declared `needs: [build-gateway]` — each test job depended on the *other* service's build artifact instead of its own. Under GitLab's `needs:`-based DAG scheduling this also means a test job could start before the build job that actually matters to it has finished.
- **Found via:** Reading each job's `needs:` against its own `script:` (`test-gateway` `cd`s into `services/gateway` but needed `build-pinger`'s artifacts). Confirmed the fix live: pipeline run against a local GitLab runner showed both `test-gateway` and `test-pinger` passing only after correcting this.
- **Fix:** `test-gateway` now needs `build-gateway`; `test-pinger` now needs `build-pinger`.

### 17. `package-gateway`/`package-pinger` had no Docker daemon to build against

- **Broken:** Both package-stage jobs run `docker build`/`docker save` inside a `docker:24` image, but nothing provided a Docker daemon for that CLI to talk to — no `docker:24-dind` service, no `DOCKER_HOST`.
- **Found via:** First real pipeline run (see below) got past build/test, and the package jobs were the next thing that would fail without this — confirmed by adding the fix and re-running: `docker build` completed, `docker save` produced `gateway.tar`/`pinger.tar`, and both were uploaded as job artifacts (visible in the GitLab job trace and via `Uploading artifacts as "archive" to coordinator... 201 Created`).
- **Fix:** Added `services: [docker:24-dind]` to both package jobs, plus `DOCKER_HOST: tcp://docker:2375` and `DOCKER_TLS_CERTDIR: ""` in `variables:` (the dind service is reachable at hostname `docker`; TLS is disabled since no certs are provisioned for it).

### 18. Live pipeline verification surfaced a real, but out-of-scope, deploy-stage bug

- **Observed:** `deploy-to-cluster` fails with `error: unknown command "sh" for "kubectl"` — the `bitnami/kubectl:latest` image's entrypoint is `kubectl` itself, not a shell, so the runner's `sh -c "..."` script wrapper doesn't work against it.
- **Not fixed here:** the README explicitly scopes "fix the deploy pipeline stage" to Tier 3, and there's no real staging cluster/`KUBECONFIG` in this local test setup anyway (`kubectl config use-context $KUBECONFIG` has nothing to point at). Left as-is; noted here since it was observed directly during Tier 2 verification and will need an image/entrypoint fix in Tier 3.

### 19. Compose had no restart policy — a crashed container just stayed dead

- **Broken:** None of the three services declared `restart:`. A crashed container (OOM, panic, any non-user-initiated exit) stayed `Exited` until someone manually ran `docker compose up` again, which doesn't meet "services start reliably."
- **Found via:** `grep restart: docker-compose.yml` → nothing. Verified the gap by killing pinger's actual host-level process (simulating a real crash, as opposed to `docker kill`/`docker exec kill`, which Docker treats as an intentional user stop and — correctly — does *not* auto-restart under `unless-stopped`): container stayed `Exited` with `RestartCount=0`.
- **Fix:** Added `restart: unless-stopped` to all three services. Re-ran the same crash simulation (SIGKILL on the container's host-level PID): container came back `Up`/`healthy` within seconds, `RestartCount=1`. Chose `unless-stopped` over `always` deliberately — it still respects a real `docker compose stop`/`down`, whereas `always` would fight a deliberate shutdown too.

### 20. Compose's obsolete `version:` key

- **Broken:** `docker-compose.yml` still had `version: "3.8"` at the top, which modern Compose ignores and warns about on every invocation (`the attribute version is obsolete, it will be ignored, please remove it to avoid potential confusion`).
- **Found via:** The warning printed on every `docker compose` command.
- **Fix:** Removed it; `docker compose config` still validates cleanly without it.

### Redis persistence (re-verified for Tier 2's "data persists correctly" goal)

Tier 1 fixes #3/#4 (bind address and `dir`/volume mount) already made this work; Tier 2 re-verified it empirically rather than just re-reading the config: wrote a key via `redis-cli SET`, ran `BGSAVE`, then did a full `docker compose down` (removes containers, keeps the named volume) + `docker compose up -d`, and confirmed the key was still readable afterward. Persistence survives a full container recreation, not just a process restart.

### Verification (Tier 2 goals)

- **CI pipeline correctly configured:** verified against a real pipeline run on a local GitLab CE + Docker-executor runner (not just YAML review) — `build-gateway`, `build-pinger`, `test-gateway`, `test-pinger`, `package-gateway`, `package-pinger` all report `success`, with `gateway.tar`/`pinger.tar` uploaded as artifacts. (`deploy-to-cluster` fails for the reason in #18, which is Tier 3 scope.)
- **Compose starts reliably:** `docker compose up -d --build` → all 3 healthy (as in Tier 1); additionally, a simulated crash of any service now results in automatic recovery via `restart: unless-stopped`, confirmed by killing pinger's host-level process and observing `RestartCount=1` and a return to `Up (healthy)`.
- **Redis data persists correctly:** confirmed via the write → `BGSAVE` → `docker compose down` → `docker compose up -d` → read round-trip described above.

---

## Tier 3 — Make it production-ready

### 21. Dev overlay: broken `bases:` path plus a patch that targeted the wrong resource kind

- **Broken:** `k8s/overlays/dev/kustomization.yaml` used the long-deprecated `bases: [../../bases]` field, pointing at a directory that has never existed (`k8s/base/`, not `k8s/bases/`), and had no `apiVersion`/`kind` header at all. `kubectl kustomize k8s/overlays/dev/` failed outright before even reaching the patch. Separately, `patches/resource-limits.yaml` targeted `kind: DaemonSet name: gateway`, but gateway is a `Deployment` (`k8s/base/gateway/deployment.yaml`) — the patch would never have matched anything even with the path fixed.
- **Found via:** `kubectl kustomize` on the overlay failing fast with `accumulating resources from ../../bases: ... no such file or directory`; comparing the patch's `kind` against the actual base resource surfaced the second bug.
- **Fix:** Switched to `resources: [../../base]` with a proper `apiVersion: kustomize.config.k8s.io/v1beta1` / `kind: Kustomization` header, and changed the patch's `kind` to `Deployment`. Verified for real against the `devops-assessment` kind cluster: `kubectl apply -k k8s/overlays/dev/` now succeeds, and `kubectl get deploy gateway -o jsonpath='{.spec.template.spec.containers[0].resources}'` on the live pod shows the patched `100m/128Mi` requests and `500m/256Mi` limits, not the base's smaller defaults.

### 22. Staging overlay: gateway replicas patched to zero

- **Broken:** `k8s/overlays/staging/patches/replicas.yaml` set `replicas: 0` on the gateway Deployment. Applying the staging overlay left the entrypoint service with zero running pods and zero Service endpoints — completely unreachable, and directly contradicted by the same overlay's `resource-limits.yaml` bumping gateway's CPU/memory up (implying it was meant to actually run, and at real capacity).
- **Found via:** `kubectl kustomize k8s/overlays/staging/` rendered `spec.replicas: 0` plainly.
- **Fix:** Changed to `replicas: 2` — enough to exercise rolling updates and basic redundancy without over-provisioning a staging environment. Verified for real: `kubectl apply -k k8s/overlays/staging/` rolled out 2/2 ready gateway pods, and `kubectl get endpoints gateway` showed two live pod IPs behind the Service.

### 23. Deploy stage: `bitnami/kubectl` entrypoint and a broken `use-context` call

- **Broken:** Two separate bugs, either fatal on its own. First: `bitnami/kubectl:latest`'s image `ENTRYPOINT` is `kubectl` itself (confirmed via `docker inspect`), not a shell — GitLab's docker executor wraps every job script in a shell invocation, so it effectively became `kubectl sh -c "..."`, failing immediately with `error: unknown command "sh" for "kubectl"` (first observed live during Tier 2's pipeline verification). Second: `kubectl config use-context $KUBECONFIG` passes the *file path* held in the `KUBECONFIG` env var as if it were a context *name* — kubectl already reads `KUBECONFIG` natively and uses that file's `current-context`, so this line does nothing useful and would itself fail once the first bug was fixed.
- **Found via:** Live pipeline trace for bug 1; reading how `KUBECONFIG` is actually consumed by `kubectl` for bug 2.
- **Fix:** Set the job image to `{name: bitnami/kubectl:latest, entrypoint: [""]}` so the runner's generated shell script executes normally, and replaced the broken `use-context` call with `kubectl config current-context` (a safe, informative sanity check — the right cluster is already selected via `KUBECONFIG` alone). Verified for real: connected the local kind cluster's control-plane container to the same Docker network as the local GitLab runner, registered a `file`-type `KUBECONFIG` CI/CD variable pointing at it (server address rewritten to the container's network alias), and ran the pipeline — `deploy-to-cluster` printed `kind-devops-assessment` as the current context, applied `k8s/overlays/staging/`, and both `rollout status` checks completed successfully against the live cluster. Full pipeline went green end-to-end for the first time this session (all 7 jobs, including deploy).

### 24. Image tagging: `:latest` in the k8s manifests, and nowhere for the CI-built image to go

- **Broken:** CI already computed `IMAGE_TAG: $CI_COMMIT_SHA` and tagged built images with it, but only ever `docker save`d them to an artifact tarball nothing downstream consumed — no `docker push` anywhere. Meanwhile `k8s/base/gateway/deployment.yaml` / `pinger/deployment.yaml` hardcoded `:latest`, completely disconnected from that SHA tag. Net effect: no reproducible link between "what CI built" and "what's deployed," and no way to identify or roll back to a specific prior build.
- **Found via:** Reading `package-gateway`/`package-pinger`'s `script:` end-to-end — the tarball artifact is produced and uploaded but never referenced by any later job or deploy step.
- **Fix:** `package-gateway`/`package-pinger` now build, tag, and `docker push` `$CI_REGISTRY_IMAGE/{gateway,pinger}:$IMAGE_TAG` to the project's GitLab Container Registry using GitLab's auto-injected `$CI_REGISTRY*` variables. `deploy-to-cluster` appends a Kustomize `images:` override to the staging overlay immediately before `kubectl apply -k`, retargeting both images to the exact tag just pushed. `k8s/base` deliberately keeps `:latest` — that's still correct for the README's local kind dev loop, which has no CI-derived SHA to use. See `docs/trade-offs.md` §1 for the full reasoning.
- **Verified, including the `docker push` leg itself:** initially the local GitLab test rig's Container Registry wasn't enabled, so `$CI_REGISTRY_IMAGE` came back empty and `docker push` failed with `invalid reference format` — confirmed that was a local-sandbox gap, not a pipeline bug, then closed it: enabled the registry (`registry['enable'] = true` + `registry_external_url` in the instance's `gitlab.rb`, `gitlab-ctl reconfigure`), fixed its token-auth realm to use the runner-reachable internal hostname instead of `localhost` (`registry['token_realm']`, same class of issue as the earlier `clone_url` fix), and re-ran the pipeline. `package-gateway`/`package-pinger` both went green with real `docker login`/`docker push` output (`<digest>: digest: sha256:... size: ...`), and the GitLab Container Registry API confirmed both `gateway` and `pinger` repositories now exist with pushed manifests. Separately, the deploy-time tag-override mechanism was verified live against the `devops-assessment` kind cluster: built an image under a distinct test tag, `kind load`-ed it, applied a scratch copy of the staging overlay with the same `images:` override the CI script generates, and confirmed via `kubectl get deploy/pods -o jsonpath` that the *running pod* had exactly that tag, not `:latest`. (The registry-enablement steps are local test-rig instance configuration only — nothing in this repo's committed files depends on them; `$CI_REGISTRY_IMAGE` etc. are populated automatically on any real GitLab project with Container Registry enabled, which is the default on gitlab.com and the real graded assessment repo.)

### Verification (Tier 3 goals)

- **Kustomize overlays fixed:** both `k8s/overlays/dev/` and `k8s/overlays/staging/` build and apply cleanly against the live `devops-assessment` kind cluster, with patches actually taking effect on the running pods (confirmed via `kubectl get ... -o jsonpath`, not just `kubectl kustomize` output).
- **Deploy pipeline stage fixed:** full pipeline (`build` → `test` → `package` → `deploy`) went green end-to-end against the local GitLab CE + runner, including `deploy-to-cluster` actually applying manifests and confirming rollout status against a real cluster.
- **Image tagging strategy proposed and fully made real:** see fix #24 and `docs/trade-offs.md` §1 — both the `docker push` and the deploy-time retag are live-verified end-to-end against the local GitLab CE + runner and the `devops-assessment` kind cluster.
- `docs/trade-offs.md` added, covering image tagging strategy, security improvements, observability recommendations, and secrets management.
