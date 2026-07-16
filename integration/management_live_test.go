package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

// TestMCPAccountShareMutationsLive is intentionally opt-in and operates only
// on unique dsmctl-e2e-* resources created during this test. It refuses cleanup
// unless the current UID/GID/UUID matches the identifier captured after create.
// If an intermediate assertion fails, it leaves the new test resource in place
// for manual inspection instead of attempting uncertain cleanup.
func TestMCPAccountShareMutationsLive(t *testing.T) {
	if os.Getenv("DSMCTL_LIVE_MUTATIONS") != "1" {
		t.Skip("set DSMCTL_LIVE_MUTATIONS=1 after authorizing temporary dsmctl-e2e-* mutations")
	}
	binary := os.Getenv("DSMCTL_MCP_BINARY")
	nas := os.Getenv("DSMCTL_LIVE_NAS")
	if binary == "" || nas == "" {
		t.Skip("set DSMCTL_MCP_BINARY and DSMCTL_LIVE_NAS to run the live mutation test")
	}

	suffix := randomHex(t, 4)
	userName := "dsmctl-e2e-u-" + suffix
	groupName := "dsmctl-e2e-g-" + suffix
	shareName := "dsmctl-e2e-s-" + suffix
	secretName := "DSMCTL_E2E_NEW_USER_PASSWORD"

	args := []string{}
	if configPath := os.Getenv("DSMCTL_LIVE_CONFIG"); configPath != "" {
		args = append(args, "--config", configPath)
	}
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), secretName+"="+randomPassword(t))
	client := mcp.NewClient(&mcp.Implementation{Name: "dsmctl-live-mutation-test", Version: "0.1.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server: %v", err)
	}
	defer session.Close()

	initialIdentity := liveIdentityState(t, ctx, session, nas)
	if _, found := liveUser(initialIdentity, userName); found {
		t.Fatalf("refusing test because user %q already exists", userName)
	}
	if _, found := liveGroup(initialIdentity, groupName); found {
		t.Fatalf("refusing test because group %q already exists", groupName)
	}
	initialShares := liveShareState(t, ctx, session, nas, false)
	if _, found := liveShare(initialShares, shareName); found {
		t.Fatalf("refusing test because shared folder %q already exists", shareName)
	}
	volumePath := ""
	for _, folder := range initialShares.Shares {
		if strings.HasPrefix(folder.VolumePath, "/volume") {
			volumePath = folder.VolumePath
			break
		}
	}
	if volumePath == "" {
		t.Fatal("no local /volume* path was discovered; refusing to guess a create target")
	}

	groupDescription := "temporary dsmctl MCP integration group"
	applyAccount(t, ctx, session, planAccount(t, ctx, session, nas, identity.ChangeRequest{
		Action:   identity.ActionCreate,
		Resource: identity.ResourceGroup,
		Group:    &identity.GroupChange{Name: groupName, Description: &groupDescription},
	}))
	createdIdentity := liveIdentityState(t, ctx, session, nas)
	createdGroup, found := liveGroup(createdIdentity, groupName)
	if !found || createdGroup.ID == "" {
		t.Fatalf("created group %q has no stable GID: %#v", groupName, createdGroup)
	}
	groupID := createdGroup.ID

	updatedGroupDescription := groupDescription + " updated"
	applyAccount(t, ctx, session, planAccount(t, ctx, session, nas, identity.ChangeRequest{
		Action:   identity.ActionUpdate,
		Resource: identity.ResourceGroup,
		Group:    &identity.GroupChange{Name: groupName, Description: &updatedGroupDescription},
	}))

	userDescription := "temporary dsmctl MCP integration user"
	userEmail := userName + "@example.invalid"
	normal := "normal"
	falseValue := false
	trueValue := true
	applyAccount(t, ctx, session, planAccount(t, ctx, session, nas, identity.ChangeRequest{
		Action:   identity.ActionCreate,
		Resource: identity.ResourceUser,
		User: &identity.UserChange{
			Name:                 userName,
			Description:          &userDescription,
			Email:                &userEmail,
			Expired:              &normal,
			CannotChangePassword: &falseValue,
			PasswordNeverExpires: &trueValue,
			CredentialRef:        "env:" + secretName,
		},
	}))
	createdIdentity = liveIdentityState(t, ctx, session, nas)
	createdUser, found := liveUser(createdIdentity, userName)
	if !found || createdUser.ID == "" {
		t.Fatalf("created user %q has no stable UID: %#v", userName, createdUser)
	}
	userID := createdUser.ID

	updatedUserDescription := userDescription + " updated"
	applyAccount(t, ctx, session, planAccount(t, ctx, session, nas, identity.ChangeRequest{
		Action:   identity.ActionUpdate,
		Resource: identity.ResourceUser,
		User: &identity.UserChange{
			Name:        userName,
			Description: &updatedUserDescription,
		},
	}))

	shareDescription := "temporary dsmctl MCP integration share"
	quota := uint64(64)
	applyShare(t, ctx, session, planShare(t, ctx, session, nas, share.ChangeRequest{
		Action:   share.ActionCreate,
		Resource: share.ResourceShare,
		Share: &share.ShareChange{
			Name:                shareName,
			VolumePath:          volumePath,
			Description:         &shareDescription,
			Hidden:              &falseValue,
			RecycleBin:          &trueValue,
			RecycleBinAdminOnly: &trueValue,
			HideUnreadable:      &trueValue,
			QuotaMiB:            &quota,
		},
	}))
	createdShares := liveShareState(t, ctx, session, nas, false)
	createdShare, found := liveShare(createdShares, shareName)
	if !found || createdShare.UUID == "" {
		t.Fatalf("created shared folder %q has no stable UUID: %#v", shareName, createdShare)
	}
	shareUUID := createdShare.UUID

	updatedShareDescription := shareDescription + " updated"
	updatedQuota := uint64(128)
	applyShare(t, ctx, session, planShare(t, ctx, session, nas, share.ChangeRequest{
		Action:   share.ActionUpdate,
		Resource: share.ResourceShare,
		Share: &share.ShareChange{
			Name:        shareName,
			Description: &updatedShareDescription,
			Hidden:      &trueValue,
			QuotaMiB:    &updatedQuota,
		},
	}))

	applyShare(t, ctx, session, planShare(t, ctx, session, nas, share.ChangeRequest{
		Action:   share.ActionSet,
		Resource: share.ResourcePermission,
		Permission: &share.PermissionChange{
			PrincipalType: share.PrincipalUser,
			Principal:     userName,
			Permissions:   []share.PermissionGrant{{ShareName: shareName, Access: share.AccessWrite}},
		},
	}))
	permissionState := liveShareState(t, ctx, session, nas, true)
	permissionShare, found := liveShare(permissionState, shareName)
	if !found || liveAccess(permissionShare, share.PrincipalUser, userName) != share.AccessWrite {
		t.Fatalf("write permission was not observed for user %q on %q", userName, shareName)
	}

	// Cleanup begins only after every create/update/permission assertion passes.
	// Each delete plan must contain the stable identifier captured after create.
	currentShares := liveShareState(t, ctx, session, nas, false)
	currentShare, found := liveShare(currentShares, shareName)
	if !found || currentShare.UUID != shareUUID {
		t.Fatalf("refusing share cleanup: current UUID=%q, created UUID=%q", currentShare.UUID, shareUUID)
	}
	shareDelete := planShare(t, ctx, session, nas, share.ChangeRequest{Action: share.ActionDelete, Resource: share.ResourceShare, Share: &share.ShareChange{Name: shareName}})
	if shareDelete.Precondition.ResourceID != shareUUID {
		t.Fatalf("refusing share cleanup: plan UUID=%q, created UUID=%q", shareDelete.Precondition.ResourceID, shareUUID)
	}
	applyShare(t, ctx, session, shareDelete)

	currentIdentity := liveIdentityState(t, ctx, session, nas)
	currentUser, found := liveUser(currentIdentity, userName)
	if !found || currentUser.ID != userID {
		t.Fatalf("refusing user cleanup: current UID=%q, created UID=%q", currentUser.ID, userID)
	}
	userDelete := planAccount(t, ctx, session, nas, identity.ChangeRequest{Action: identity.ActionDelete, Resource: identity.ResourceUser, User: &identity.UserChange{Name: userName}})
	if userDelete.Precondition.ResourceID != userID {
		t.Fatalf("refusing user cleanup: plan UID=%q, created UID=%q", userDelete.Precondition.ResourceID, userID)
	}
	applyAccount(t, ctx, session, userDelete)

	currentIdentity = liveIdentityState(t, ctx, session, nas)
	currentGroup, found := liveGroup(currentIdentity, groupName)
	if !found || currentGroup.ID != groupID {
		t.Fatalf("refusing group cleanup: current GID=%q, created GID=%q", currentGroup.ID, groupID)
	}
	groupDelete := planAccount(t, ctx, session, nas, identity.ChangeRequest{Action: identity.ActionDelete, Resource: identity.ResourceGroup, Group: &identity.GroupChange{Name: groupName}})
	if groupDelete.Precondition.ResourceID != groupID {
		t.Fatalf("refusing group cleanup: plan GID=%q, created GID=%q", groupDelete.Precondition.ResourceID, groupID)
	}
	applyAccount(t, ctx, session, groupDelete)

	finalIdentity := liveIdentityState(t, ctx, session, nas)
	finalShares := liveShareState(t, ctx, session, nas, false)
	if _, found := liveUser(finalIdentity, userName); found {
		t.Errorf("temporary user %q remains after cleanup", userName)
	}
	if _, found := liveGroup(finalIdentity, groupName); found {
		t.Errorf("temporary group %q remains after cleanup", groupName)
	}
	if _, found := liveShare(finalShares, shareName); found {
		t.Errorf("temporary shared folder %q remains after cleanup", shareName)
	}
}

func planAccount(t *testing.T, ctx context.Context, session *mcp.ClientSession, nas string, request identity.ChangeRequest) application.IdentityPlan {
	t.Helper()
	var output struct {
		Plan application.IdentityPlan `json:"plan"`
	}
	callLiveTool(t, ctx, session, "plan_account_change", map[string]any{"nas": nas, "request": request}, &output)
	return output.Plan
}

func applyAccount(t *testing.T, ctx context.Context, session *mcp.ClientSession, plan application.IdentityPlan) {
	t.Helper()
	var output struct {
		Result application.IdentityApplyResult `json:"result"`
	}
	callLiveTool(t, ctx, session, "apply_account_plan", map[string]any{"plan": plan, "approval_hash": plan.Hash}, &output)
	if !output.Result.Applied || output.Result.PlanHash != plan.Hash {
		t.Fatalf("unexpected account apply result: %#v", output.Result)
	}
}

func planShare(t *testing.T, ctx context.Context, session *mcp.ClientSession, nas string, request share.ChangeRequest) application.SharePlan {
	t.Helper()
	var output struct {
		Plan application.SharePlan `json:"plan"`
	}
	callLiveTool(t, ctx, session, "plan_share_change", map[string]any{"nas": nas, "request": request}, &output)
	return output.Plan
}

func applyShare(t *testing.T, ctx context.Context, session *mcp.ClientSession, plan application.SharePlan) {
	t.Helper()
	var output struct {
		Result application.ShareApplyResult `json:"result"`
	}
	callLiveTool(t, ctx, session, "apply_share_plan", map[string]any{"plan": plan, "approval_hash": plan.Hash}, &output)
	if !output.Result.Applied || output.Result.PlanHash != plan.Hash {
		t.Fatalf("unexpected share apply result: %#v", output.Result)
	}
}

func liveIdentityState(t *testing.T, ctx context.Context, session *mcp.ClientSession, nas string) synology.IdentityState {
	t.Helper()
	var output struct {
		Identity synology.IdentityState `json:"identity"`
	}
	callLiveTool(t, ctx, session, "get_account_state", map[string]any{"nas": nas}, &output)
	return output.Identity
}

func liveShareState(t *testing.T, ctx context.Context, session *mcp.ClientSession, nas string, permissions bool) synology.ShareState {
	t.Helper()
	var output struct {
		Shares synology.ShareState `json:"shares"`
	}
	callLiveTool(t, ctx, session, "get_share_state", map[string]any{"nas": nas, "include_permissions": permissions}, &output)
	return output.Shares
}

func callLiveTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any, output any) {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if result.IsError {
		content, _ := json.Marshal(result.Content)
		t.Fatalf("call %s returned tool error: %s", name, content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode %s result: %v", name, err)
	}
	if err := json.Unmarshal(data, output); err != nil {
		t.Fatalf("decode %s result: %v", name, err)
	}
}

func liveUser(state synology.IdentityState, name string) (identity.User, bool) {
	for _, user := range state.Users {
		if user.Name == name {
			return user, true
		}
	}
	return identity.User{}, false
}

func liveGroup(state synology.IdentityState, name string) (identity.Group, bool) {
	for _, group := range state.Groups {
		if group.Name == name {
			return group, true
		}
	}
	return identity.Group{}, false
}

func liveShare(state synology.ShareState, name string) (share.SharedFolder, bool) {
	for _, folder := range state.Shares {
		if folder.Name == name {
			return folder, true
		}
	}
	return share.SharedFolder{}, false
}

func liveAccess(folder share.SharedFolder, principalType, principal string) string {
	for _, permission := range folder.Permissions {
		if permission.PrincipalType == principalType && permission.Principal == principal {
			return permission.Access
		}
	}
	return share.AccessNone
}

func randomHex(t *testing.T, bytes int) string {
	t.Helper()
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(value)
}

func randomPassword(t *testing.T) string {
	t.Helper()
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate random password: %v", err)
	}
	return "Aa1!" + base64.RawURLEncoding.EncodeToString(value)
}
