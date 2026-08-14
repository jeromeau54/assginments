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
