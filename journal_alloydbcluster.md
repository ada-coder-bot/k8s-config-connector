# Journal: AlloyDBCluster Migration Diff Discrepancies Investigation and Fix

## Overview
Investigation and resolution of false-positive field diff discrepancies (`labels`, `initial_user.password`, `initial_user.user`) between Terraform and Direct controllers for `alloydb.cnrm.cloud.google.com/AlloyDBCluster` during migration.

## Step 1: Reproduction of the Bugs

### Analysis
- **Labels**:
  - In `AlloyDBCluster` direct controller (`pkg/controller/direct/alloydb/alloydbcluster_controller.go`), `desiredPb.Labels` was populated directly from KRM object metadata (`a.desired.GetObjectMeta().GetLabels()`) and manually set `"managed-by-cnrm": "true"`.
  - It did NOT use `label.GCPLabels(u)` (which filters out KRM prefix labels like `cnrm.cloud.google.com/*`).
  - In `Update`, the controller was doing a raw proto diff `common.CompareProtoMessage(desiredPb, a.actual, common.BasicDiff)` instead of `common.CompareBrownfieldSpecAndLabels`. Because KRM metadata contains annotations and labels that are not in GCP `a.actual.Labels`, or because GCP system labels / test labels differed, raw proto diff produced false-positive diffs on `labels`.
- **Initial User (`initial_user.user`, `initial_user.password`)**:
  - `initial_user` is an input-only / create-only field for initial cluster user setup.
  - GCP API does not return `initial_user` in `GetCluster` (it is unreadable / write-only).
  - During `Update`, `desiredPb.InitialUser` had values populated from KRM spec, while `a.actual.InitialUser` is `nil`.
  - Raw proto diff detected diffs on `initial_user.user` and `initial_user.password`, resulting in unnecessary `PATCH` calls with `updateMask=initialUser` during migration takeover and re-reconciliation.

### Test Runs & Reproduction Results
1. Ran `TestMigrationToDirect` for `basicalloydbcluster`:
   - Command: `KUBEBUILDER_ASSETS=/root/.local/share/kubebuilder-envtest/k8s/1.37.0-linux-amd64 E2E_KUBE_TARGET=envtest E2E_GCP_TARGET=mock GOLDEN_REQUEST_CHECKS=1 GOLDEN_OBJECT_CHECKS=1 WRITE_GOLDEN_OUTPUT=1 RUN_E2E=1 go test ./tests/e2e -v -run 'TestMigrationToDirect/fixtures/basicalloydbcluster$'`
   - Reproduction in `_migration_diffs.json`:
     - Discrepancy observed: `initial_user.password`, `initial_user.user` showed diffs.
   - Reproduction in `_http_migration_phase3_direct_takeover.log`:
     - Non-GET call observed: `PATCH https://alloydb.googleapis.com/v1beta/projects/${projectId}/locations/southamerica-east1/clusters/alloydbcluster${uniqueId}?%24alt=json%3Benum-encoding%3Dint&updateMask=initialUser`
