// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package oslogin

import (
	"context"
	"fmt"
	"strings"

	gcp "cloud.google.com/go/oslogin/apiv1"
	osloginpb "cloud.google.com/go/oslogin/apiv1/osloginpb"
	commonpb "cloud.google.com/go/oslogin/common/commonpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/oslogin/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
)

func init() {
	registry.RegisterModel(krm.OSLoginSSHPublicKeyGVK, NewOSLoginSSHPublicKeyModel)
}

func NewOSLoginSSHPublicKeyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelOSLoginSSHPublicKey{config: *config}, nil
}

var _ directbase.Model = &modelOSLoginSSHPublicKey{}

type modelOSLoginSSHPublicKey struct {
	config config.ControllerConfig
}

func (m *modelOSLoginSSHPublicKey) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}
	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building OSLogin REST client: %w", err)
	}
	return gcpClient, nil
}

func (m *modelOSLoginSSHPublicKey) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	obj := &krm.OSLoginSSHPublicKey{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}

	id, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	sshPublicKeyID := id.(*krm.OSLoginSSHPublicKeyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}

	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	mapCtx := &direct.MapContext{}
	desired := OSLoginSSHPublicKeySpec_v1alpha1_ToProto(mapCtx, &obj.Spec)
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	return &osloginSSHPublicKeyAdapter{
		id:             sshPublicKeyID,
		gcpClient:      gcpClient,
		desired:        desired,
		desiredProject: obj.Spec.Project,
	}, nil
}

func (m *modelOSLoginSSHPublicKey) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.OSLoginSSHPublicKeyIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}

	return &osloginSSHPublicKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

type osloginSSHPublicKeyAdapter struct {
	id             *krm.OSLoginSSHPublicKeyIdentity
	gcpClient      *gcp.Client
	desired        *commonpb.SshPublicKey
	desiredProject *string
	actual         *commonpb.SshPublicKey
}

var _ directbase.Adapter = &osloginSSHPublicKeyAdapter{}

func (a *osloginSSHPublicKeyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting OSLoginSSHPublicKey", "name", a.id)

	if a.id.Fingerprint == "" {
		return false, nil
	}

	req := &osloginpb.GetSshPublicKeyRequest{Name: a.id.String()}
	actual, err := a.gcpClient.GetSshPublicKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting OSLoginSSHPublicKey %q: %w", a.id, err)
	}

	a.actual = actual
	return true, nil
}

func (a *osloginSSHPublicKeyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating OSLoginSSHPublicKey", "name", a.id)

	parent := fmt.Sprintf("users/%s", a.id.User)

	req := &osloginpb.ImportSshPublicKeyRequest{
		Parent:       parent,
		SshPublicKey: a.desired,
	}
	if a.desiredProject != nil {
		req.ProjectId = *a.desiredProject
	}

	resp, err := a.gcpClient.ImportSshPublicKey(ctx, req)
	if err != nil {
		return fmt.Errorf("creating OSLoginSSHPublicKey %s: %w", a.id, err)
	}

	var created *commonpb.SshPublicKey
	if resp.LoginProfile != nil && len(resp.LoginProfile.SshPublicKeys) > 0 {
		if a.id.Fingerprint != "" {
			created = resp.LoginProfile.SshPublicKeys[a.id.Fingerprint]
		}
		if created == nil {
			// Find matching key by key content
			for _, k := range resp.LoginProfile.SshPublicKeys {
				if strings.TrimSpace(k.GetKey()) == strings.TrimSpace(a.desired.GetKey()) {
					created = k
					break
				}
			}
		}
		// If still not found and only one key exists, take it
		if created == nil && len(resp.LoginProfile.SshPublicKeys) == 1 {
			for _, k := range resp.LoginProfile.SshPublicKeys {
				created = k
				break
			}
		}
	}

	var fingerprint string
	if created != nil {
		fingerprint = created.GetFingerprint()
	}

	if fingerprint == "" {
		return fmt.Errorf("could not determine fingerprint for created OSLoginSSHPublicKey %s", a.id)
	}

	a.id.Fingerprint = fingerprint
	if err := createOp.SetSpecResourceID(ctx, fingerprint); err != nil {
		return err
	}

	// Fetch full resource from server to ensure complete state
	getReq := &osloginpb.GetSshPublicKeyRequest{Name: a.id.String()}
	latest, err := a.gcpClient.GetSshPublicKey(ctx, getReq)
	if err != nil {
		return fmt.Errorf("fetching created OSLoginSSHPublicKey %s: %w", a.id, err)
	}

	return a.updateStatus(ctx, createOp, latest)
}

func (a *osloginSSHPublicKeyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating OSLoginSSHPublicKey", "name", a.id.String())

	diffs, updateMask, err := a.diff(ctx)
	if err != nil {
		return err
	}

	if !diffs.HasDiff() {
		log.V(2).Info("no field needs update", "name", a.id.String())
		return a.updateStatus(ctx, updateOp, a.actual)
	}

	structuredreporting.ReportDiff(ctx, diffs)

	req := &osloginpb.UpdateSshPublicKeyRequest{
		Name:         a.id.String(),
		SshPublicKey: a.desired,
		UpdateMask:   updateMask,
	}

	updated, err := a.gcpClient.UpdateSshPublicKey(ctx, req)
	if err != nil {
		return fmt.Errorf("updating OSLoginSSHPublicKey %s: %w", a.id.String(), err)
	}

	return a.updateStatus(ctx, updateOp, updated)
}

func (a *osloginSSHPublicKeyAdapter) diff(ctx context.Context) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	mapCtx := &direct.MapContext{}

	maskedActualSpec := OSLoginSSHPublicKeySpec_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return nil, nil, mapCtx.Err()
	}
	maskedActual := OSLoginSSHPublicKeySpec_v1alpha1_ToProto(mapCtx, maskedActualSpec)
	if mapCtx.Err() != nil {
		return nil, nil, mapCtx.Err()
	}

	clonedDesired := proto.Clone(a.desired).(*commonpb.SshPublicKey)

	return common.DiffForTopLevelFields(ctx, clonedDesired.ProtoReflect(), maskedActual.ProtoReflect())
}

func (a *osloginSSHPublicKeyAdapter) updateStatus(ctx context.Context, op directbase.Operation, latest *commonpb.SshPublicKey) error {
	mapCtx := &direct.MapContext{}
	status := OSLoginSSHPublicKeyStatus_v1alpha1_FromProto(mapCtx, latest)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	return op.UpdateStatus(ctx, status, nil)
}

func (a *osloginSSHPublicKeyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.OSLoginSSHPublicKey{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(OSLoginSSHPublicKeySpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	obj.Spec.User = a.id.User
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Fingerprint)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Fingerprint)
	u.SetGroupVersionKind(krm.OSLoginSSHPublicKeyGVK)

	return u, nil
}

func (a *osloginSSHPublicKeyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting OSLoginSSHPublicKey", "name", a.id)

	req := &osloginpb.DeleteSshPublicKeyRequest{Name: a.id.String()}
	err := a.gcpClient.DeleteSshPublicKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			log.V(2).Info("skipping delete for non-existent OSLoginSSHPublicKey, assuming it was already deleted", "name", a.id)
			return true, nil
		}
		return false, fmt.Errorf("deleting OSLoginSSHPublicKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted OSLoginSSHPublicKey", "name", a.id)

	return true, nil
}
