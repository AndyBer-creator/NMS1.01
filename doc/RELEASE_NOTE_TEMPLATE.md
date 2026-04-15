# Release Note Template (Incidents / Trap Mapping)

Используйте этот шаблон для релизов, которые затрагивают:
- incident lifecycle/API/UI;
- trap correlation/dedup;
- `trap_oid_mappings` schema/rules/UI;
- incident SLA dashboards / alerting semantics.

---

## 1) Release Meta

- Release version/tag:
- Date/time (UTC):
- Owner/on-call:
- Related PR(s):
- Rollout scope (stage/prod/both):

---

## 2) Change Summary

- What changed (1-3 bullets):
- Why it changed (business/operations rationale):
- Backward compatibility statement:
  - [ ] no API changes
  - [ ] backward-compatible API changes
  - [ ] breaking changes (must be detailed below)

---

## 3) API & Contract Impact

- Endpoints affected:
- OpenAPI schemas affected:
- Contract checks:
  - `make contract-http-spec` status:
  - `make openapi-breaking-check` status:
- Client impact:
  - [ ] none
  - [ ] update required
  - [ ] migration window required

If breaking change:
- Exact breaking behavior:
- Mitigation / migration guide:
- Planned removal/deprecation date:

---

## 4) Data / Migration Impact

- DB migrations included:
- Affected tables/indexes:
- Expected migration time:
- Data backfill needed:
  - [ ] no
  - [ ] yes (describe)
- Rollback complexity:
  - [ ] low
  - [ ] medium
  - [ ] high

---

## 5) Operational Impact Notes

### Incident lifecycle / processing
- New transition rules or constraints:
- Dedup/correlation behavior changes:
- Auto-resolve behavior changes:

### Trap mapping changes
- New/changed vendor rules:
- Priority/enable flag changes:
- Fallback behavior impact:

### Dashboards / alerting
- New/changed panels:
- Metric semantic changes:
- Alert threshold implications:

### Security / runtime
- New env vars required:
- Insecure flags forbidden/changed:
- Production profile changes:

---

## 6) Validation Evidence

- Tests:
  - `go test ./...`:
  - Integration tests (if applicable):
- Smoke:
  - `make e2e-http-smoke`:
  - `make e2e-auth-smoke`:
- Optional load/chaos:
  - `make load-http-readonly` / `make k6-*`:
  - `make chaos-worker-check`:

Artifacts/logs:
- CI run URL:
- Grafana screenshot links (optional):
- Contract report/log snippet:

---

## 7) Rollout & Rollback Plan

Rollout steps:
1.
2.
3.

Rollback trigger conditions:
- 

Rollback command/doc reference:
- `doc/ROLLBACK.md` section:

Post-rollback checks:
- 

---

## 8) Communication

- Stakeholders notified:
- User-visible changes summary:
- Runbook updates required:
  - [ ] no
  - [ ] yes (list docs/files)

---

## 9) Sign-off

- Engineering:
- QA/Validation:
- Ops/SRE:
- Product/Owner (if required):
