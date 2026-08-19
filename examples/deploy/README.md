# Example Deployments

This directory contains practical deployment examples for the current project.

## Install Driver Components with Helm

```bash
helm upgrade --install gitrepo-csi-driver ./helm/gitrepo-csi-driver -n git-content-system --create-namespace
```

## Example PHP Deployments

- `php-dynamic-site-tag.yaml`: immutable release style using a tag
- `php-dynamic-site-branch.yaml`: mutable branch tracking style

## Revision Update Behaviour

The current implementation materialises content at Pod start.

- Commit SHA: immutable; no refresh expected.
- Tag or branch: mutable by definition; content updates are picked up on Pod restart.

### Recommended Strategy

- Production: pin a commit SHA and roll out intentionally.
- Staging: use tags and roll out when release tags move.
- Development: use branches and restart Deployments when upstream changes are needed.

### Example restart command for branch tracking

```bash
kubectl rollout restart deployment php-dynamic-branch
```

### Optional automated refresh using CronJob

`rollout-restart-cronjob.yaml` provides a lightweight restart cadence for mutable refs.
