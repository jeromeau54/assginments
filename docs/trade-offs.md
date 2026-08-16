# Trade-offs (Tier 3)

Design decisions and recommendations for taking this stack from "works" to "production-ready." Written against what's actually in this repo, not generic advice.

---

## 1. Image tagging strategy

**What was there:** `k8s/base/gateway/deployment.yaml` and `k8s/base/pinger/deployment.yaml` hardcoded `devops/gateway:latest` / `devops/pinger:latest`. The CI pipeline already computed a real identity for each build (`IMAGE_TAG: $CI_COMMIT_SHA`) and tagged the built images with it — but then saved them to a `.tar` artifact that nothing downstream ever consumed, and the deploy stage applied manifests that still said `:latest`, completely disconnected from what CI had just built.

**Why `:latest` doesn't work for "production-ready":**
- **Not reproducible.** Two people (or two pipeline runs) applying the same YAML at different times can end up running different code with no diff to point at.
- **No rollback story.** If the current deploy is bad, there's no previous tag to roll back to — `:latest` only ever means "whatever was pushed most recently."
- **`imagePullPolicy: IfNotPresent` interacts badly with it.** Fix #14 in `docs/fixes.md` set `IfNotPresent` specifically so `kind load docker-image` works without a registry. In a real cluster with a registry, `:latest` + `IfNotPresent` means a node that already has *some* `:latest` image locally will happily keep running stale code and never re-pull, silently diverging from what other nodes are running.
- **No audit trail.** Given a running pod, there's no way to answer "which commit is this?" without cross-referencing deploy timestamps against CI history and hoping nothing raced.

**What I implemented:** commit `feat(ci): push SHA-tagged images to the registry and deploy that exact tag`.
- `package-gateway`/`package-pinger` now build, tag, and push `$CI_REGISTRY_IMAGE/{gateway,pinger}:$CI_COMMIT_SHA` to the project's GitLab Container Registry, using GitLab's auto-injected `$CI_REGISTRY*` variables — no manual credential setup required on a real project.
- `deploy-to-cluster` overrides the deployed image (name *and* tag) via Kustomize's `images:` transformer immediately before `kubectl apply -k`, so staging always runs the exact image that pipeline run just built and pushed — never `:latest`.
- `k8s/base` deliberately **keeps** `:latest` as the default. That's not an oversight: the README's documented local workflow (`make docker-images && kind load docker-image devops/gateway:latest ...`) has no CI run to derive a SHA from, so `latest` is the correct default for a laptop/kind dev loop. The override only kicks in for the CI-driven deploy, which is exactly where reproducibility matters.
- Verified live, both halves: the deploy-time retag was proven first — built an image under a distinct test tag, `kind load`-ed it into the `devops-assessment` cluster, applied a scratch copy of the staging overlay with the same `images:` override the CI script generates, and confirmed via `kubectl get deploy -o jsonpath` that the *running pod* had that exact tag. The `docker push` leg initially couldn't be verified — the local GitLab test rig's Container Registry wasn't enabled, so `$CI_REGISTRY_IMAGE` came back empty and the push failed with `invalid tag "/gateway:...": invalid reference format`, confirming that was a local-sandbox gap rather than a pipeline bug. Closed it rather than leaving it as a caveat: enabled the registry on the test instance (`registry['enable'] = true` + `registry_external_url`, `gitlab-ctl reconfigure`) and fixed its token-auth realm to use the runner-reachable internal hostname instead of `localhost` (`registry['token_realm']` — the same class of fix as the `clone_url` override needed to get the runner cloning at all). Re-ran the pipeline: `package-gateway`/`package-pinger` both produced real `docker push` output (`<digest>: digest: sha256:... size: ...`), and the registry's own API confirmed `gateway`/`pinger` repositories now exist with pushed manifests. (That registry setup is local test-instance configuration, not anything in this repo — `$CI_REGISTRY_IMAGE` etc. are populated automatically on any real GitLab project with Container Registry enabled, the default on gitlab.com and the actual graded assessment repo.)

**If I had more room:** move `IMAGE_TAG` to something that also encodes intent for humans, e.g. `${CI_COMMIT_SHORT_SHA}` for readability in `kubectl get pods` output, or a semantic-release tag on top of SHA for anything cut as an actual release — SHA alone is the right *machine* source of truth but a poor human-facing label on a dashboard.

---

## 2. Security improvements

Roughly in the order I'd actually do them:

1. **Redis has no authentication.** `services/redis/redis.conf` / `k8s/base/redis/configmap.yaml` both set `protected-mode no` with no `requirepass`. Tier 1's fix log already flags this as an accepted trade-off for a network-isolated Compose setup, but in Kubernetes the blast radius is bigger — any pod in the `assessment` namespace (or any namespace, without a NetworkPolicy — see #2 below) can read/write cache data or issue `FLUSHALL`. I'd add `requirepass` sourced from a Secret (see §4) and set `REDISCLI_AUTH`/`REDIS_PASSWORD` in gateway's and pinger's env so they authenticate.

2. **No NetworkPolicies.** Every pod in the `assessment` namespace can currently reach every other pod, and (absent a default-deny at the cluster level) pods in *other* namespaces can reach `redis`/`gateway`/`pinger` too. I'd add a default-deny-ingress NetworkPolicy for the namespace, then explicit allow rules: gateway ← ingress/external, pinger ← gateway only, redis ← gateway+pinger only.

3. **No resource `limits` enforcement beyond what's set, and no PodDisruptionBudgets.** Limits exist (fix'd further in the Tier 3 overlay work), but there's no PDB, so a node drain during a staging deploy could take both gateway replicas down simultaneously despite `replicas: 2`. Cheap to add, meaningfully improves availability during routine cluster maintenance.

4. **Containers already run as non-root (`runAsUser: 1000`, fix #1/#17 in Tier 1) but don't set `readOnlyRootFilesystem`, drop Linux capabilities, or set `allowPrivilegeEscalation: false`.** Both Go binaries are static and don't need to write anywhere except maybe `/tmp`; I'd add a full `securityContext` (`readOnlyRootFilesystem: true`, `capabilities: {drop: [ALL]}`, `allowPrivilegeEscalation: false`) plus an `emptyDir` mount for `/tmp` if either binary needs scratch space.

5. **`package-gateway`/`package-pinger` run as `privileged: true` via the dind service** (needed for Docker-in-Docker to work at all). This is the standard trade-off for building images in GitLab CI without a separate build service, but it does mean a compromised build script has host-level blast radius on the runner. The safer long-term fix is `kaniko` or `buildah` (rootless, no privileged dind needed) — didn't switch to it here to keep the Tier 2 CI verification scope contained, but it's the natural next step.

6. **No image scanning.** I'd add a `trivy image $CI_REGISTRY_IMAGE/gateway:$IMAGE_TAG` (or GitLab's built-in Container Scanning) step after the push, failing the pipeline on HIGH/CRITICAL CVEs before `deploy-to-cluster` ever runs.

---

## 3. Observability recommendations

The services already expose `/healthz` and `/readyz` (used by both Compose healthchecks and k8s probes), but that's liveness/readiness, not observability:

- **Structured logging.** No indication either Go service logs in a structured format. I'd standardize on JSON logs to stdout (12-factor — no log files to manage) with at minimum `level`, `msg`, `service`, and a request ID, so they're directly ingestible by whatever the cluster's log pipeline is (Loki, or the EFK/ELK stack, or the platform's hosted logging).
- **Metrics.** Neither service appears to expose a `/metrics` endpoint. I'd add `promhttp` (Go's Prometheus client) to both, at minimum: request count/latency histograms for gateway's proxy path, and pinger's own ping success/failure counters and latency to gateway — pinger is *already* a synthetic health-checker, it's a natural fit to also emit `target_up`/`ping_latency_seconds` as real Prometheus metrics rather than only writing the result into Redis.
- **Tracing.** Gateway proxies to pinger and both talk to Redis — a single slow request currently has no way to tell you *which hop* was slow. Even basic OpenTelemetry instrumentation (gateway → pinger span propagation via a trace header) would make that answerable in minutes instead of guesswork.
- **Alerting on the CI/CD pipeline itself, not just the app.** `deploy-to-cluster`'s `environment: staging` block already gives GitLab an environment page to track deploy history/rollback from — worth actually using (`environment: { on_stop: ... }` for review-app-style teardown, and a Slack/webhook notification on pipeline failure on `main`).
- **Dashboards should surface the two rollout-status checks that are already in CI** (`kubectl rollout status deployment/gateway|pinger`) as deploy-frequency/deploy-failure-rate metrics — they're already a pass/fail signal per deploy, just not being collected anywhere yet.

---

## 4. Secrets management approach

There are no application secrets in this stack today (Redis has no password, no API keys anywhere) — but the exercise of setting up local CI verification this session surfaced the *shape* of the problem directly, worth recording:

- **What I actually did locally, as a cautionary example:** to verify the CI pipeline end-to-end, I generated a GitLab Personal Access Token for the local test instance and, at one point, embedded it directly in a `git remote` URL (`http://root:<token>@localhost:8929/...`) so I could push without an interactive prompt. That's exactly the anti-pattern real secrets management has to prevent — the token sat in `.git/config` in plaintext and would have been exposed by `git remote -v`, shell history, or process listings. It was fine here because it's a throwaway local instance with no real data behind it, but it's the wrong pattern for anything real, and I called it out explicitly in `CLAUDE.md` for this session so it's never treated as a template to copy from.
- **For the real pipeline (`KUBECONFIG`, registry creds):** GitLab CI/CD variables of type **File** (used for `KUBECONFIG` in this session's verification) or **Variable** (masked + protected) are the right primitive — never commit a kubeconfig or registry token to the repo, and never echo them in job scripts (`kubectl config current-context` was chosen deliberately over dumping the full kubeconfig for debugging). `$CI_REGISTRY_USER`/`$CI_REGISTRY_PASSWORD` used for the registry push are GitLab-managed and job-scoped automatically — nothing to rotate or leak on the project's end.
- **If Redis auth is added (§2.1):** the password should be a Kubernetes `Secret` (or, better, sourced from an external secrets manager — Vault, AWS Secrets Manager, or GitLab's own CI/CD variables synced via `external-secrets` — rather than a raw `Secret` object, which is only base64-encoded, not encrypted, at rest by default unless the cluster has encryption-at-rest configured for the etcd `Secret` resource type).
- **Protect the `main`-only deploy gate.** `deploy-to-cluster` already has `only: [main]`, which is good — I'd pair that with GitLab's protected-branch + protected-variable settings so the `KUBECONFIG`/registry credentials are literally unreadable by a pipeline running on any other branch or an MR from a fork.
