# OSLoginSSHPublicKey Direct Controller Journal

## Implementation Details

- **GAPIC REST Client**: Used `cloud.google.com/go/oslogin/apiv1` REST client initialized via `m.config.RESTClientOptions()`.
- **Identity & Creation**: OSLoginSSHPublicKey utilizes server-generated resource IDs (fingerprint). During `Create()`, keys are imported via `ImportSshPublicKeyRequest` to the parent user path `users/{user}`. The resulting fingerprint is captured and assigned to `spec.resourceID` via `createOp.SetSpecResourceID()`.
- **Update Handling**: Implemented `Update()` using `UpdateSshPublicKeyRequest` and `tags.DiffForTopLevelFields` for computing the top-level field mask (such as `expirationTimeUsec`).
- **Export Support**: Implemented `AdapterForURL` and `Export()`.
- **Testing & Alignment**: Added `k8s.ReconcilerTypeDirect` to `SupportedControllers` for `OSLoginSSHPublicKey` in `pkg/controller/resourceconfig/static_config.go`. Verified the direct controller alongside the fallback legacy controller in E2E mock tests. Golden mock files (`_generated_object_osloginsshpublickey.golden.yaml`, `_http_mock.log`, `_http_old_controller_mock.log`, diffs) were generated and verified.
