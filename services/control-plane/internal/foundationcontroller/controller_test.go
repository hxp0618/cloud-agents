package foundationcontroller

import (
	"errors"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
)

func TestClassifyEffectResult(t *testing.T) {
	for _, test := range []struct {
		err        error
		attempt    int32
		transition string
		code       string
	}{
		{nil, 1, "succeeded", ""},
		{opensandbox.ErrUnavailable, 1, "retry", "foundation_runtime_unavailable"},
		{opensandbox.ErrUnavailable, 8, "failed", "foundation_runtime_unavailable"},
		{opensandbox.ErrRuntimeFailed, 1, "failed", "opensandbox_runtime_failed"},
		{opensandbox.ErrPolicyUnenforced, 1, "failed", "foundation_network_policy_unenforced"},
		{dockertarget.ErrDeploymentConflict, 1, "failed", "foundation_ownership_conflict"},
		{errors.Join(opensandbox.ErrInvalid, errors.New("secret")), 1, "failed", "foundation_configuration_invalid"},
	} {
		transition, code := classify(test.err, test.attempt)
		if transition != test.transition || code != test.code {
			t.Fatalf("transition/code = %s/%s", transition, code)
		}
	}
}
