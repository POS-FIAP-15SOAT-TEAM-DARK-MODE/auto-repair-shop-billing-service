# ADR 0003 — Git branching strategy: feature → develop → main

- Status: Accepted
- Date: 2026-08-22
- Deciders: Giusier F.
- Tags: workflow, ci-cd, governance

## Context

Our engineering governance policy requires, across all 4 repositories: a protected `main`/`master` branch with no direct commits, and mandatory Pull Requests for merges. This repo's `docker.yml` also treats `main` and `develop` as deploy targets (`PRD` and `STG` GitHub Environments respectively), so branch structure is not just a code-review convention here — it directly gates what gets deployed where.

Before this decision, `main` had branch protection (1 required approval) but `develop` did not, and there was no explicit rule for where feature branches should merge first.

## Decision

Adopt a two-stage flow:
- Feature/fix branches → PR into `develop`.
- `develop` → PR into `main`, promoting reviewed/working changes (this merge is also a **production deploy trigger**, not just a code promotion, given `docker.yml`'s branch-to-environment mapping).

Both `main` and `develop` are GitHub branch-protected: PRs required to merge (no direct pushes), enforced even for repo admins, no force-push, no branch deletion. `main` additionally requires 1 approving review (unchanged from before this ADR); `develop` requires 0 approvals so solo work isn't blocked while still going through review-visible history.

## Consequences

### Positive

- Matches our governance policy across all 4 repos.
- `develop`→`main` merges are a deliberate, visible checkpoint — appropriate given that merge also triggers a `PRD` deploy.
- Consistent convention across all 4 repos simplifies onboarding a reviewer who works across more than one of them.

### Negative

- One extra hop (feature → develop → main) versus merging features straight to `main`, adds latency for small fixes.
- `develop`'s 0-required-approvals setting means a single contributor can merge without external review at that stage — the real review gate is the `develop`→`main` promotion.

## Alternatives considered

- **A. Trunk-based (feature → main directly).** Simpler, fewer PRs, but doesn't give a safe "integration" branch to accumulate and review changes before a `PRD`-triggering merge.
- **B. GitFlow with release branches.** More ceremony (separate `release/*` branches) than a small team needs; two-stage flow captures the useful part (a review-friendly staging branch) without the extra overhead.

## Notes

- This same ADR (adapted per repo) is recorded in `auto-repair-shop-infra-db`, `auto-repair-shop-infra-k8s`, and `auto-repair-shop-lambda-auth` — those repos don't have a `develop`/`main` deploy-environment mapping (they use Terraform workspaces, `stg`/`prd`, selected as a workflow input, not by branch), but the same PR-required governance rule applies to keep all 4 repos consistent.
