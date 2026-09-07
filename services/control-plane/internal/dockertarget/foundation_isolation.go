package dockertarget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

type networkInspect struct {
	ID       string            `json:"Id"`
	Internal bool              `json:"Internal"`
	Options  map[string]string `json:"Options"`
}

// VerifyFoundationSandboxIsolation proves the Docker runtime and deny-only
// network used by a shared-untrusted Sandbox instead of trusting node claims.
func (directory *CredentialDirectory) VerifyFoundationSandboxIsolation(
	ctx context.Context, endpoint, credentialRef, runtimeID string,
) error {
	if ctx == nil || commonv1alpha1.ValidateIdentifier(runtimeID, "/runtimeId") != nil {
		return ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	filter, _ := json.Marshal(map[string][]string{"label": {"opensandbox.io/id=" + runtimeID}})
	var containers []containerSummary
	if err := dockerJSON(ctx, client, http.MethodGet, base+"/containers/json?all=1&filters="+url.QueryEscape(string(filter)), nil, http.StatusOK, &containers); err != nil {
		return err
	}
	if len(containers) != 1 {
		return ErrIsolationUnenforced
	}
	container, err := inspectWorkerContainer(ctx, client, base, containers[0].ID)
	if err != nil || !container.State.Running || container.Config.Labels["opensandbox.io/id"] != runtimeID ||
		container.HostConfig.Runtime != "runsc" || len(container.NetworkSettings.Networks) != 1 {
		return ErrIsolationUnenforced
	}
	for _, attachment := range container.NetworkSettings.Networks {
		var network networkInspect
		if attachment.NetworkID == "" || dockerJSON(ctx, client, http.MethodGet, base+"/networks/"+url.PathEscape(attachment.NetworkID), nil, http.StatusOK, &network) != nil ||
			network.ID != attachment.NetworkID || !network.Internal || network.Options["com.docker.network.bridge.enable_icc"] != "false" {
			return ErrIsolationUnenforced
		}
	}
	return nil
}
