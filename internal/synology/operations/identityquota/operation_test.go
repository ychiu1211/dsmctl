package identityquota

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type quotaExecutor struct {
	response json.RawMessage
	request  compatibility.Request
}

func (executor *quotaExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.request = request
	return executor.response, nil
}

func TestReadDecodesShareQuotasAndSetUsesMiBIntegers(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(APIName, compatibility.APIInfo{MinVersion: 1, MaxVersion: 1})
	executor := &quotaExecutor{response: json.RawMessage(`{"user_quota":[{"volume":"/volume1","quota_status":"v2","shares":[{"name":"projects","quota":2048},{"name":"archive","quota":0}]}]}`)}
	quota, _, err := ExecuteRead(context.Background(), target, executor, ReadInput{PrincipalType: identity.PrincipalUser, Principal: "alice"})
	if err != nil {
		t.Fatalf("ExecuteRead() error = %v", err)
	}
	if len(quota.Limits) != 2 || quota.Limits[1].Target != "projects" || quota.Limits[1].QuotaMiB != 2048 || quota.Limits[1].Volume != "/volume1" {
		t.Fatalf("quota = %#v", quota)
	}
	executor.response = json.RawMessage(`{}`)
	_, _, err = ExecuteSet(context.Background(), target, executor, SetInput{PrincipalType: identity.PrincipalGroup, Principal: "dev", Limits: []identity.QuotaLimitChange{{TargetType: identity.QuotaTargetVolume, Target: "/volume1", QuotaMiB: 4096}}})
	if err != nil {
		t.Fatalf("ExecuteSet() error = %v", err)
	}
	items, ok := executor.request.JSONParameters["group_quota"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["volume"] != "/volume1" || items[0]["quota"] != int64(4096) {
		t.Fatalf("group_quota = %#v", executor.request.JSONParameters["group_quota"])
	}
}
