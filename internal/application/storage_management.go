package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

var ErrStorageMutationBackendUnavailable = errors.New("storage mutation backend is not available; no DSM request was sent")

// StoragePlan is the stable approval artifact for future storage backends. Its
// hash covers the canonical request, every referenced stable DSM ID, the
// observed resource, and the normalized storage topology.
type StoragePlan struct {
	APIVersion              string                          `json:"api_version" jsonschema:"Plan schema version"`
	NAS                     string                          `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision         uint64                          `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request                 storage.ChangeRequest           `json:"request" jsonschema:"Validated and canonical storage change intent"`
	Precondition            ChangePrecondition              `json:"precondition" jsonschema:"Observed resource state that must still match during apply"`
	References              StorageStableReferences         `json:"references" jsonschema:"Stable DSM identifiers resolved while planning"`
	TopologyFingerprint     string                          `json:"topology_fingerprint" jsonschema:"Hash of stable disk, pool, and volume topology observed during planning"`
	SafetyFingerprint       string                          `json:"safety_fingerprint" jsonschema:"Hash of selected disk health and pool actionability observations that must remain unchanged until apply"`
	ResolvedCapacityBytes   uint64                          `json:"resolved_capacity_bytes,omitempty" jsonschema:"Explicit byte capacity resolved from an exact or maximum policy during planning"`
	ApplicableRAIDTypes     []string                        `json:"applicable_raid_types,omitempty" jsonschema:"Model- and selected-disk-count RAID choices calculated during pool creation planning"`
	Destructive             bool                            `json:"destructive" jsonschema:"Whether the plan can destroy data or replace an existing topology"`
	Risk                    string                          `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings                []string                        `json:"warnings" jsonschema:"Storage-specific warnings that must be reviewed"`
	DestructiveConsequences []StorageDestructiveConsequence `json:"destructive_consequences" jsonschema:"Data-bearing resources that may be destroyed"`
	Summary                 []string                        `json:"summary" jsonschema:"Human-readable actions the plan will perform"`
	Hash                    string                          `json:"hash" jsonschema:"SHA-256 approval hash covering the complete canonical plan"`
}

type StorageStableReferences struct {
	ResourceID      string   `json:"resource_id,omitempty" jsonschema:"Stable DSM ID of the pool, volume, or cache being changed"`
	PoolID          string   `json:"pool_id,omitempty" jsonschema:"Stable DSM storage-pool ID referenced by the intent"`
	PoolPath        string   `json:"pool_path,omitempty" jsonschema:"Stable DSM pool_path captured while planning a volume change"`
	SpacePath       string   `json:"space_path,omitempty" jsonschema:"Stable DSM space_path captured while planning a single-volume deployment"`
	PoolLayout      string   `json:"pool_layout,omitempty" jsonschema:"DSM pool volume layout captured while planning"`
	DiskIDs         []string `json:"disk_ids,omitempty" jsonschema:"Sorted stable DSM disk IDs participating in the topology"`
	CacheID         string   `json:"cache_id,omitempty" jsonschema:"Stable DSM SSD cache ID captured while planning a cache change"`
	CacheVolumePath string   `json:"cache_volume_path,omitempty" jsonschema:"Parent volume identifier DSM addresses the SSD cache by"`
}

type StorageDestructiveConsequence struct {
	Kind        string   `json:"kind" jsonschema:"Destructive consequence category"`
	ResourceIDs []string `json:"resource_ids" jsonschema:"Sorted stable DSM resource IDs affected by the consequence"`
	Description string   `json:"description" jsonschema:"Human-readable consequence"`
}

// StorageApplyResult is shared by CLI and MCP. Pool operations return success
// only after a fresh inventory verifies their operation-specific postcondition.
type StorageApplyResult struct {
	NAS       string                         `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                         `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                           `json:"applied" jsonschema:"Whether a backend applied and verified the plan"`
	Operation synology.StorageMutationResult `json:"operation" jsonschema:"Selected DSM operation result after postcondition verification"`
}

type storageManagementClient interface {
	StorageState(context.Context) (synology.StorageState, error)
	StorageCapabilities(context.Context) (synology.StorageCapabilities, synology.CompatibilityReport, error)
	ApplyStorageChange(context.Context, synology.StorageMutationInput) (synology.StorageMutationResult, error)
}

func (s *Service) PlanStorageChange(ctx context.Context, requestedNAS string, request storage.ChangeRequest) (StoragePlan, error) {
	if err := ctx.Err(); err != nil {
		return StoragePlan{}, err
	}
	canonical, err := canonicalStorageRequest(request)
	if err != nil {
		return StoragePlan{}, err
	}
	if err := ensureStorageBackendScope(canonical); err != nil {
		return StoragePlan{}, err
	}
	if s.manager == nil {
		return StoragePlan{}, ErrStorageMutationBackendUnavailable
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return StoragePlan{}, err
	}
	plan, err := planStorageChangeWithClient(ctx, name, client, canonical)
	if err != nil {
		return StoragePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = storagePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyStoragePlan(ctx context.Context, plan StoragePlan, approvalHash string) (StorageApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return StorageApplyResult{}, err
	}
	if err := validateStoragePlan(plan, approvalHash); err != nil {
		return StorageApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return StorageApplyResult{}, err
	}
	if err := ensureStorageBackendScope(plan.Request); err != nil {
		return StorageApplyResult{}, err
	}
	if s.manager == nil {
		return StorageApplyResult{}, ErrStorageMutationBackendUnavailable
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return StorageApplyResult{}, err
	}
	name, client, err := s.manager.Client(ctx, plan.NAS)
	if err != nil {
		return StorageApplyResult{}, err
	}
	if name != plan.NAS {
		return StorageApplyResult{}, fmt.Errorf("storage plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyStoragePlanWithClient(ctx, client, plan)
}

func ensureStorageBackendScope(request storage.ChangeRequest) error {
	if request.Resource == storage.ResourcePool && request.Action == storage.ActionUpdate && request.Pool != nil && request.Pool.TargetRAIDType != nil {
		return fmt.Errorf("storage-pool RAID migration is not implemented; no DSM request was sent")
	}
	if request.Resource != storage.ResourcePool && request.Resource != storage.ResourceVolume && request.Resource != storage.ResourceCache {
		return ErrStorageMutationBackendUnavailable
	}
	return nil
}

func planStorageChangeWithClient(ctx context.Context, nas string, client storageManagementClient, request storage.ChangeRequest) (StoragePlan, error) {
	capabilities, _, err := client.StorageCapabilities(ctx)
	if err != nil {
		return StoragePlan{}, authenticationError(nas, err)
	}
	if !storageActionSupported(capabilities, request.Resource, request.Action) {
		return StoragePlan{}, fmt.Errorf("NAS %q does not support storage %s %s through a verified backend: %w", nas, request.Resource, request.Action, ErrStorageMutationBackendUnavailable)
	}
	state, err := client.StorageState(ctx)
	if err != nil {
		return StoragePlan{}, authenticationError(nas, err)
	}
	return BuildStoragePlan(nas, state, request)
}

func applyStoragePlanWithClient(ctx context.Context, client storageManagementClient, plan StoragePlan) (StorageApplyResult, error) {
	capabilities, _, err := client.StorageCapabilities(ctx)
	if err != nil {
		return StorageApplyResult{}, authenticationError(plan.NAS, err)
	}
	if !storageActionSupported(capabilities, plan.Request.Resource, plan.Request.Action) {
		return StorageApplyResult{}, fmt.Errorf("NAS %q no longer supports storage %s %s through a verified backend: %w", plan.NAS, plan.Request.Resource, plan.Request.Action, ErrStorageMutationBackendUnavailable)
	}
	current, err := client.StorageState(ctx)
	if err != nil {
		return StorageApplyResult{}, authenticationError(plan.NAS, err)
	}
	refreshed, err := BuildStoragePlan(plan.NAS, current, plan.Request)
	if err != nil {
		return StorageApplyResult{}, fmt.Errorf("storage plan precondition failed: %w", err)
	}
	refreshed.ProfileRevision = plan.ProfileRevision
	refreshed.Hash, err = storagePlanHash(refreshed)
	if err != nil {
		return StorageApplyResult{}, err
	}
	if refreshed.Hash != plan.Hash || refreshed.TopologyFingerprint != plan.TopologyFingerprint || refreshed.SafetyFingerprint != plan.SafetyFingerprint ||
		!reflect.DeepEqual(refreshed.Precondition, plan.Precondition) || !reflect.DeepEqual(refreshed.References, plan.References) {
		return StorageApplyResult{}, fmt.Errorf("storage plan is stale; inventory or selected disk/pool safety state changed since planning")
	}

	operation, err := client.ApplyStorageChange(ctx, synology.StorageMutationInput{
		Request: plan.Request, State: current, ResolvedCapacityBytes: plan.ResolvedCapacityBytes,
	})
	if err != nil {
		return StorageApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.StorageState(ctx)
	if err != nil {
		return StorageApplyResult{}, fmt.Errorf("NAS %q mutation returned but postcondition inventory failed: %w", plan.NAS, err)
	}
	resourceID, err := verifyStoragePostcondition(plan, current, after)
	if err != nil {
		return StorageApplyResult{}, fmt.Errorf("NAS %q storage %s postcondition failed: %w", plan.NAS, plan.Request.Resource, err)
	}
	if operation.ResourceID == "" {
		operation.ResourceID = resourceID
	}
	return StorageApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func storageActionSupported(capabilities storage.Capabilities, resource, action string) bool {
	if resource == storage.ResourceCache {
		switch action {
		case storage.ActionCreate:
			return capabilities.CacheCreate
		case storage.ActionUpdate:
			return capabilities.CacheExpand
		case storage.ActionConvert:
			return capabilities.CacheConvert
		case storage.ActionDelete:
			return capabilities.CacheDelete
		default:
			return false
		}
	}
	if resource == storage.ResourceVolume {
		switch action {
		case storage.ActionCreate:
			return capabilities.VolumeCreate
		case storage.ActionUpdate:
			return capabilities.VolumeUpdate
		case storage.ActionDelete:
			return capabilities.VolumeDelete
		default:
			return false
		}
	}
	switch action {
	case storage.ActionCreate:
		return capabilities.PoolCreate
	case storage.ActionUpdate:
		return capabilities.PoolUpdate
	case storage.ActionDelete:
		return capabilities.PoolDelete
	default:
		return false
	}
}

// BuildStoragePlan is a pure planner over normalized inventory. It performs no
// I/O and is the contract boundary future DSM operation variants plug into.
func BuildStoragePlan(nas string, state storage.State, request storage.ChangeRequest) (StoragePlan, error) {
	nas = strings.TrimSpace(nas)
	if nas == "" {
		return StoragePlan{}, fmt.Errorf("NAS profile is required")
	}
	canonical, err := canonicalStorageRequest(request)
	if err != nil {
		return StoragePlan{}, err
	}
	topology, err := normalizeStorageTopology(state)
	if err != nil {
		return StoragePlan{}, err
	}
	if err := validateStorageTopologyReferences(topology, canonical); err != nil {
		return StoragePlan{}, err
	}
	applicableRAIDTypes, resolvedCapacityBytes, err := validateStorageMutationSafety(state, topology, canonical)
	if err != nil {
		return StoragePlan{}, err
	}
	safetyFingerprint, err := storageSafetyFingerprint(state, canonical)
	if err != nil {
		return StoragePlan{}, err
	}

	plan := StoragePlan{
		APIVersion:            managementAPIVersion,
		NAS:                   nas,
		Request:               canonical,
		TopologyFingerprint:   fingerprint(topology),
		SafetyFingerprint:     safetyFingerprint,
		ResolvedCapacityBytes: resolvedCapacityBytes,
		ApplicableRAIDTypes:   applicableRAIDTypes,
	}
	plan.Precondition, plan.References = storagePreconditionAndReferences(topology, canonical)
	plan.Destructive, plan.Warnings, plan.DestructiveConsequences, plan.Summary = storagePlanEffects(topology, canonical, resolvedCapacityBytes)
	plan.Risk = riskLevel(plan.Destructive)
	plan.Hash, err = storagePlanHash(plan)
	if err != nil {
		return StoragePlan{}, err
	}
	return plan, nil
}

type storageTopology struct {
	Disks   []storageTopologyDisk   `json:"disks"`
	Pools   []storageTopologyPool   `json:"pools"`
	Volumes []storageTopologyVolume `json:"volumes"`
	Caches  []storageTopologyCache  `json:"caches,omitempty"`
}

// storageTopologyCache carries the cache identity facts that must invalidate a
// stale plan: a parent-volume change, a mode flip, or an SSD add/remove.
type storageTopologyCache struct {
	ID             string   `json:"id"`
	VolumeID       string   `json:"volume_id,omitempty"`
	CacheType      string   `json:"cache_type,omitempty"`
	ProtectionRAID string   `json:"protection_raid,omitempty"`
	DiskIDs        []string `json:"disk_ids"`
	SizeBytes      uint64   `json:"size_bytes,omitempty"`
}

type storageTopologyDisk struct {
	ID        string `json:"id"`
	Serial    string `json:"serial,omitempty"`
	SizeBytes uint64 `json:"size_bytes,omitempty"`
}

type storageTopologyPool struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	Path      string   `json:"path,omitempty"`
	SpacePath string   `json:"space_path,omitempty"`
	RAIDType  string   `json:"raid_type,omitempty"`
	Layout    string   `json:"layout,omitempty"`
	DiskIDs   []string `json:"disk_ids"`
	SizeBytes uint64   `json:"size_bytes,omitempty"`
}

type storageTopologyVolume struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	PoolID             string `json:"pool_id,omitempty"`
	FileSystem         string `json:"file_system,omitempty"`
	SizeBytes          uint64 `json:"size_bytes,omitempty"`
	AllocatedBytes     uint64 `json:"allocated_bytes,omitempty"`
	MaxFileSystemBytes uint64 `json:"max_file_system_bytes,omitempty"`
	ReadOnly           bool   `json:"read_only"`
}

type storageSafetyObservation struct {
	PoolCreation   storage.PoolCreationConstraints   `json:"pool_creation"`
	VolumeCreation storage.VolumeCreationConstraints `json:"volume_creation"`
	CacheCreation  *storage.CacheCreationConstraints `json:"cache_creation,omitempty"`
	Disks          []storageSafetyDisk               `json:"disks,omitempty"`
	Pool           *storageSafetyPool                `json:"pool,omitempty"`
	Volume         *storageSafetyVolume              `json:"volume,omitempty"`
	Cache          *storageSafetyCache               `json:"cache,omitempty"`
}

type storageSafetyCache struct {
	ID               string   `json:"id"`
	VolumeID         string   `json:"volume_id"`
	CacheType        string   `json:"cache_type"`
	ProtectionRAID   string   `json:"protection_raid,omitempty"`
	Status           string   `json:"status"`
	Health           string   `json:"health"`
	ProtectionStatus string   `json:"protection_status,omitempty"`
	DiskIDs          []string `json:"disk_ids"`
	HasDirtyData     bool     `json:"has_dirty_data"`
	Flushing         bool     `json:"flushing"`
	Actioning        bool     `json:"actioning"`
	CanDelete        bool     `json:"can_delete"`
	SizeBytes        uint64   `json:"size_bytes,omitempty"`
}

type storageSafetyDisk struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Health        string `json:"health"`
	SMARTStatus   string `json:"smart_status,omitempty"`
	UsedBy        string `json:"used_by,omitempty"`
	InUse         bool   `json:"in_use"`
	Selectable    bool   `json:"selectable"`
	Compatibility string `json:"compatibility,omitempty"`
	Serial        string `json:"serial,omitempty"`
	Firmware      string `json:"firmware,omitempty"`
	SizeBytes     uint64 `json:"size_bytes,omitempty"`
}

type storageSafetyPool struct {
	ID              string   `json:"id"`
	RAIDType        string   `json:"raid_type"`
	Status          string   `json:"status"`
	Health          string   `json:"health"`
	DiskIDs         []string `json:"disk_ids"`
	Writable        bool     `json:"writable"`
	Actioning       bool     `json:"actioning"`
	CanExpand       bool     `json:"can_expand"`
	CanDelete       bool     `json:"can_delete"`
	Compatible      bool     `json:"compatible"`
	CanCreateVolume bool     `json:"can_create_volume"`
	SizeBytes       uint64   `json:"size_bytes,omitempty"`
	UsedBytes       uint64   `json:"used_bytes,omitempty"`
	AvailableBytes  uint64   `json:"available_bytes,omitempty"`
	Path            string   `json:"path,omitempty"`
	SpacePath       string   `json:"space_path,omitempty"`
	Layout          string   `json:"layout,omitempty"`
	MaxDiskCount    int      `json:"max_disk_count,omitempty"`
}

type storageSafetyVolume struct {
	ID                 string `json:"id"`
	PoolID             string `json:"pool_id"`
	FileSystem         string `json:"file_system"`
	Status             string `json:"status"`
	Health             string `json:"health"`
	ReadOnly           bool   `json:"read_only"`
	Writable           bool   `json:"writable"`
	Actioning          bool   `json:"actioning"`
	SingleVolume       bool   `json:"single_volume"`
	CanExpand          bool   `json:"can_expand"`
	CanDelete          bool   `json:"can_delete"`
	AllocatedBytes     uint64 `json:"allocated_bytes"`
	MaxFileSystemBytes uint64 `json:"max_file_system_bytes,omitempty"`
}

func normalizeStorageTopology(state storage.State) (storageTopology, error) {
	topology := storageTopology{
		Disks:   make([]storageTopologyDisk, 0, len(state.Disks)),
		Pools:   make([]storageTopologyPool, 0, len(state.Pools)),
		Volumes: make([]storageTopologyVolume, 0, len(state.Volumes)),
	}
	seenDisks := make(map[string]struct{}, len(state.Disks))
	for _, disk := range state.Disks {
		id := strings.TrimSpace(disk.ID)
		if id == "" {
			return storageTopology{}, fmt.Errorf("storage inventory contains a disk without a stable ID")
		}
		if _, duplicate := seenDisks[id]; duplicate {
			return storageTopology{}, fmt.Errorf("storage inventory contains duplicate disk ID %q", id)
		}
		seenDisks[id] = struct{}{}
		topology.Disks = append(topology.Disks, storageTopologyDisk{ID: id, Serial: disk.Serial, SizeBytes: disk.SizeBytes})
	}
	seenPools := make(map[string]struct{}, len(state.Pools))
	for _, pool := range state.Pools {
		id := strings.TrimSpace(pool.ID)
		if id == "" {
			return storageTopology{}, fmt.Errorf("storage inventory contains a pool without a stable ID")
		}
		if _, duplicate := seenPools[id]; duplicate {
			return storageTopology{}, fmt.Errorf("storage inventory contains duplicate pool ID %q", id)
		}
		seenPools[id] = struct{}{}
		diskIDs, err := canonicalIDs("pool disk", pool.DiskIDs)
		if err != nil {
			return storageTopology{}, fmt.Errorf("pool %q: %w", id, err)
		}
		for _, diskID := range diskIDs {
			if _, found := seenDisks[diskID]; !found {
				return storageTopology{}, fmt.Errorf("pool %q references unknown stable disk ID %q", id, diskID)
			}
		}
		topology.Pools = append(topology.Pools, storageTopologyPool{
			ID: id, Name: pool.Name, Path: strings.TrimSpace(pool.Path), SpacePath: strings.TrimSpace(pool.SpacePath),
			RAIDType: normalizeObservedRAIDType(pool.RAIDType), Layout: strings.ToLower(strings.TrimSpace(pool.Layout)),
			DiskIDs: diskIDs, SizeBytes: pool.SizeBytes,
		})
	}
	seenVolumes := make(map[string]struct{}, len(state.Volumes))
	for _, volume := range state.Volumes {
		id := strings.TrimSpace(volume.ID)
		if id == "" {
			return storageTopology{}, fmt.Errorf("storage inventory contains a volume without a stable ID")
		}
		if _, duplicate := seenVolumes[id]; duplicate {
			return storageTopology{}, fmt.Errorf("storage inventory contains duplicate volume ID %q", id)
		}
		seenVolumes[id] = struct{}{}
		poolID := strings.TrimSpace(volume.PoolID)
		if poolID != "" {
			if _, found := seenPools[poolID]; !found {
				return storageTopology{}, fmt.Errorf("volume %q references unknown stable pool ID %q", id, poolID)
			}
		}
		topology.Volumes = append(topology.Volumes, storageTopologyVolume{
			ID: id, Name: volume.Name, PoolID: poolID, FileSystem: strings.ToLower(strings.TrimSpace(volume.FileSystem)),
			SizeBytes: volume.SizeBytes, AllocatedBytes: volume.AllocatedBytes,
			MaxFileSystemBytes: volume.MaxFileSystemBytes, ReadOnly: volume.ReadOnly,
		})
	}
	seenCaches := make(map[string]struct{}, len(state.Caches))
	for _, cache := range state.Caches {
		id := strings.TrimSpace(cache.ID)
		if id == "" {
			return storageTopology{}, fmt.Errorf("storage inventory contains an SSD cache without a stable ID")
		}
		if _, duplicate := seenCaches[id]; duplicate {
			return storageTopology{}, fmt.Errorf("storage inventory contains duplicate SSD cache ID %q", id)
		}
		seenCaches[id] = struct{}{}
		diskIDs, err := canonicalIDs("cache disk", cache.DiskIDs)
		if err != nil {
			return storageTopology{}, fmt.Errorf("cache %q: %w", id, err)
		}
		topology.Caches = append(topology.Caches, storageTopologyCache{
			ID: id, VolumeID: strings.TrimSpace(cache.VolumeID), CacheType: cache.CacheType,
			ProtectionRAID: normalizeObservedRAIDType(cache.ProtectionRAID), DiskIDs: diskIDs, SizeBytes: cache.SizeBytes,
		})
	}
	sort.Slice(topology.Disks, func(i, j int) bool { return topology.Disks[i].ID < topology.Disks[j].ID })
	sort.Slice(topology.Pools, func(i, j int) bool { return topology.Pools[i].ID < topology.Pools[j].ID })
	sort.Slice(topology.Volumes, func(i, j int) bool { return topology.Volumes[i].ID < topology.Volumes[j].ID })
	sort.Slice(topology.Caches, func(i, j int) bool { return topology.Caches[i].ID < topology.Caches[j].ID })
	return topology, nil
}

func canonicalStorageRequest(request storage.ChangeRequest) (storage.ChangeRequest, error) {
	canonical := storage.ChangeRequest{
		Action:   strings.ToLower(strings.TrimSpace(request.Action)),
		Resource: strings.ToLower(strings.TrimSpace(request.Resource)),
	}
	if request.Pool != nil {
		pool := *request.Pool
		pool.ID = strings.TrimSpace(pool.ID)
		pool.Name = strings.TrimSpace(pool.Name)
		if pool.RAIDType != "" {
			var err error
			pool.RAIDType, err = canonicalRAIDType(pool.RAIDType)
			if err != nil {
				return storage.ChangeRequest{}, err
			}
		}
		var err error
		pool.DiskIDs, err = canonicalIDs("disk", pool.DiskIDs)
		if err != nil {
			return storage.ChangeRequest{}, err
		}
		pool.AddDiskIDs, err = canonicalIDs("disk", pool.AddDiskIDs)
		if err != nil {
			return storage.ChangeRequest{}, err
		}
		if pool.TargetRAIDType != nil {
			target, err := canonicalRAIDType(*pool.TargetRAIDType)
			if err != nil {
				return storage.ChangeRequest{}, err
			}
			pool.TargetRAIDType = &target
		}
		canonical.Pool = &pool
	}
	if request.Volume != nil {
		volume := *request.Volume
		volume.ID = strings.TrimSpace(volume.ID)
		volume.Name = strings.TrimSpace(volume.Name)
		volume.PoolID = strings.TrimSpace(volume.PoolID)
		volume.FileSystem = strings.ToLower(strings.TrimSpace(volume.FileSystem))
		if volume.Capacity != nil {
			capacity := *volume.Capacity
			capacity.Mode = strings.ToLower(strings.TrimSpace(capacity.Mode))
			volume.Capacity = &capacity
		}
		if volume.ExpandTo != nil {
			expandTo := *volume.ExpandTo
			expandTo.Mode = strings.ToLower(strings.TrimSpace(expandTo.Mode))
			volume.ExpandTo = &expandTo
		}
		canonical.Volume = &volume
	}
	if request.Cache != nil {
		cache := *request.Cache
		cache.ID = strings.TrimSpace(cache.ID)
		cache.Name = strings.TrimSpace(cache.Name)
		cache.VolumeID = strings.TrimSpace(cache.VolumeID)
		cache.CacheType = strings.ToLower(strings.TrimSpace(cache.CacheType))
		var err error
		cache.DiskIDs, err = canonicalIDs("disk", cache.DiskIDs)
		if err != nil {
			return storage.ChangeRequest{}, err
		}
		cache.AddDiskIDs, err = canonicalIDs("disk", cache.AddDiskIDs)
		if err != nil {
			return storage.ChangeRequest{}, err
		}
		if cache.ProtectionRAID != "" {
			cache.ProtectionRAID, err = canonicalRAIDType(cache.ProtectionRAID)
			if err != nil {
				return storage.ChangeRequest{}, err
			}
		}
		if cache.TargetMode != nil {
			mode := strings.ToLower(strings.TrimSpace(*cache.TargetMode))
			cache.TargetMode = &mode
		}
		canonical.Cache = &cache
	}
	if err := validateStorageRequestShape(canonical); err != nil {
		return storage.ChangeRequest{}, err
	}
	return canonical, nil
}

func validateStorageRequestShape(request storage.ChangeRequest) error {
	if request.Action != storage.ActionCreate && request.Action != storage.ActionUpdate && request.Action != storage.ActionConvert && request.Action != storage.ActionDelete {
		return fmt.Errorf("unsupported storage action %q", request.Action)
	}
	switch request.Resource {
	case storage.ResourcePool:
		if request.Pool == nil || request.Volume != nil || request.Cache != nil {
			return fmt.Errorf("pool resource requires exactly one pool intent")
		}
		return validatePoolChange(request.Action, *request.Pool)
	case storage.ResourceVolume:
		if request.Volume == nil || request.Pool != nil || request.Cache != nil {
			return fmt.Errorf("volume resource requires exactly one volume intent")
		}
		return validateVolumeChange(request.Action, *request.Volume)
	case storage.ResourceCache:
		if request.Cache == nil || request.Pool != nil || request.Volume != nil {
			return fmt.Errorf("cache resource requires exactly one cache intent")
		}
		return validateCacheChange(request.Action, *request.Cache)
	default:
		return fmt.Errorf("unsupported storage resource %q", request.Resource)
	}
}

// validateCacheChange enforces per-action field ownership for SSD cache intents.
// Create owns the full initial cache; update (expand) only adds SSDs; convert
// only changes the mode (and may add SSDs and a protection RAID when enabling
// read-write); delete names an existing cache by stable ID. The convert and
// update actions are shape-validated here even though a given DSM may report them
// unsupported; the capability gate rejects an unbacked action separately.
func validateCacheChange(action string, change storage.CacheChange) error {
	switch action {
	case storage.ActionCreate:
		if change.ID != "" || len(change.AddDiskIDs) != 0 || change.TargetMode != nil {
			return fmt.Errorf("cache create accepts only name, volume_id, cache_type, disk_ids, and protection_raid")
		}
		if err := validateStorageName("cache", change.Name); err != nil {
			return err
		}
		if change.VolumeID == "" {
			return fmt.Errorf("cache create requires the parent volume_id")
		}
		switch change.CacheType {
		case storage.CacheModeReadOnly:
			if change.ProtectionRAID != "" {
				return fmt.Errorf("a read-only cache does not accept protection_raid")
			}
			if len(change.DiskIDs) < 1 {
				return fmt.Errorf("a read-only cache requires at least one SSD")
			}
		case storage.CacheModeReadWrite:
			if !isCacheProtectionRAID(change.ProtectionRAID) {
				return fmt.Errorf("a read-write cache requires protection_raid of raid1, raid5, or raid6")
			}
			if err := validateRAIDDiskCount(change.ProtectionRAID, len(change.DiskIDs)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("cache create requires cache_type read_only or read_write")
		}
	case storage.ActionUpdate:
		if change.ID == "" {
			return fmt.Errorf("cache expand requires stable id")
		}
		if change.Name != "" || change.VolumeID != "" || change.CacheType != "" || len(change.DiskIDs) != 0 || change.ProtectionRAID != "" || change.TargetMode != nil {
			return fmt.Errorf("cache expand is patch-only and accepts only id and add_disk_ids")
		}
		if len(change.AddDiskIDs) == 0 {
			return fmt.Errorf("cache expand requires at least one SSD in add_disk_ids")
		}
	case storage.ActionConvert:
		if change.ID == "" {
			return fmt.Errorf("cache convert requires stable id")
		}
		if change.TargetMode == nil {
			return fmt.Errorf("cache convert requires target_mode")
		}
		if change.Name != "" || change.VolumeID != "" || change.CacheType != "" || len(change.DiskIDs) != 0 {
			return fmt.Errorf("cache convert accepts only id, target_mode, add_disk_ids, and protection_raid")
		}
		switch *change.TargetMode {
		case storage.CacheModeReadWrite:
			if change.ProtectionRAID != "" && !isCacheProtectionRAID(change.ProtectionRAID) {
				return fmt.Errorf("cache convert protection_raid must be raid1, raid5, or raid6")
			}
		case storage.CacheModeReadOnly:
			if change.ProtectionRAID != "" || len(change.AddDiskIDs) != 0 {
				return fmt.Errorf("converting to read-only accepts only id and target_mode")
			}
		default:
			return fmt.Errorf("cache convert target_mode must be read_only or read_write")
		}
	case storage.ActionDelete:
		if change.ID == "" {
			return fmt.Errorf("cache delete requires stable id")
		}
		if change.Name != "" || change.VolumeID != "" || change.CacheType != "" || len(change.DiskIDs) != 0 || len(change.AddDiskIDs) != 0 || change.ProtectionRAID != "" || change.TargetMode != nil {
			return fmt.Errorf("cache delete accepts only stable id")
		}
	}
	return nil
}

func isCacheProtectionRAID(value string) bool {
	return value == storage.RAID1 || value == storage.RAID5 || value == storage.RAID6
}

func validatePoolChange(action string, change storage.PoolChange) error {
	switch action {
	case storage.ActionCreate:
		if change.ID != "" || len(change.AddDiskIDs) != 0 || change.TargetRAIDType != nil {
			return fmt.Errorf("pool create accepts only name, raid_type, and disk_ids")
		}
		if err := validateStorageName("pool", change.Name); err != nil {
			return err
		}
		if change.RAIDType == "" {
			return fmt.Errorf("pool create requires raid_type")
		}
		if err := validateRAIDDiskCount(change.RAIDType, len(change.DiskIDs)); err != nil {
			return err
		}
	case storage.ActionUpdate:
		if change.ID == "" {
			return fmt.Errorf("pool update requires stable id")
		}
		if change.Name != "" || change.RAIDType != "" || len(change.DiskIDs) != 0 {
			return fmt.Errorf("pool update is patch-only and accepts only id, add_disk_ids, and target_raid_type")
		}
		if len(change.AddDiskIDs) == 0 && change.TargetRAIDType == nil {
			return fmt.Errorf("pool update requires at least one disk addition or target RAID type")
		}
	case storage.ActionDelete:
		if change.ID == "" {
			return fmt.Errorf("pool delete requires stable id")
		}
		if change.Name != "" || change.RAIDType != "" || len(change.DiskIDs) != 0 || len(change.AddDiskIDs) != 0 || change.TargetRAIDType != nil {
			return fmt.Errorf("pool delete accepts only stable id")
		}
	}
	return nil
}

func validateVolumeChange(action string, change storage.VolumeChange) error {
	switch action {
	case storage.ActionCreate:
		if change.ID != "" || change.ExpandTo != nil {
			return fmt.Errorf("volume create accepts only name, pool_id, file_system, and capacity")
		}
		if err := validateStorageName("volume", change.Name); err != nil {
			return err
		}
		if change.PoolID == "" {
			return fmt.Errorf("volume create requires stable pool_id")
		}
		if change.FileSystem != storage.FileSystemBtrfs && change.FileSystem != storage.FileSystemExt4 {
			return fmt.Errorf("unsupported volume file_system %q", change.FileSystem)
		}
		if change.Capacity == nil {
			return fmt.Errorf("volume create requires an explicit capacity policy")
		}
		if err := validateCapacityPolicy(*change.Capacity); err != nil {
			return fmt.Errorf("volume capacity: %w", err)
		}
	case storage.ActionUpdate:
		if change.ID == "" {
			return fmt.Errorf("volume update requires stable id")
		}
		if change.Name != "" || change.PoolID != "" || change.FileSystem != "" || change.Capacity != nil {
			return fmt.Errorf("volume update is patch-only and accepts only id and expand_to")
		}
		if change.ExpandTo == nil {
			return fmt.Errorf("volume update requires expand_to")
		}
		if err := validateCapacityPolicy(*change.ExpandTo); err != nil {
			return fmt.Errorf("volume expansion: %w", err)
		}
	case storage.ActionDelete:
		if change.ID == "" {
			return fmt.Errorf("volume delete requires stable id")
		}
		if change.Name != "" || change.PoolID != "" || change.FileSystem != "" || change.Capacity != nil || change.ExpandTo != nil {
			return fmt.Errorf("volume delete accepts only stable id")
		}
	}
	return nil
}

func validateCapacityPolicy(policy storage.CapacityPolicy) error {
	switch policy.Mode {
	case storage.CapacityMaximum:
		if policy.SizeBytes != 0 {
			return fmt.Errorf("maximum capacity requires size_bytes=0")
		}
	case storage.CapacityExact:
		if policy.SizeBytes == 0 {
			return fmt.Errorf("exact capacity requires positive size_bytes")
		}
	default:
		return fmt.Errorf("unsupported capacity mode %q", policy.Mode)
	}
	return nil
}

func validateStorageTopologyReferences(topology storageTopology, request storage.ChangeRequest) error {
	disks := make(map[string]storageTopologyDisk, len(topology.Disks))
	for _, disk := range topology.Disks {
		disks[disk.ID] = disk
	}
	occupied := make(map[string]string)
	for _, pool := range topology.Pools {
		for _, diskID := range pool.DiskIDs {
			if owner, found := occupied[diskID]; found {
				return fmt.Errorf("stable disk ID %q is assigned to both pools %q and %q", diskID, owner, pool.ID)
			}
			occupied[diskID] = pool.ID
		}
	}
	if request.Resource == storage.ResourceCache {
		return validateCacheTopologyReferences(topology, disks, occupied, request)
	}
	if request.Resource == storage.ResourcePool {
		change := request.Pool
		if request.Action == storage.ActionCreate {
			for _, pool := range topology.Pools {
				if strings.EqualFold(pool.Name, change.Name) {
					return fmt.Errorf("storage pool named %q already exists with stable ID %q", change.Name, pool.ID)
				}
			}
			return validateAvailableDisks(disks, occupied, change.DiskIDs)
		}
		pool, found := topologyPoolByID(topology, change.ID)
		if !found {
			return fmt.Errorf("storage pool with stable ID %q does not exist", change.ID)
		}
		if request.Action == storage.ActionUpdate {
			if err := validateAvailableDisks(disks, occupied, change.AddDiskIDs); err != nil {
				return err
			}
			if change.TargetRAIDType != nil {
				if normalizeObservedRAIDType(pool.RAIDType) == *change.TargetRAIDType && len(change.AddDiskIDs) == 0 {
					return fmt.Errorf("pool %q already uses RAID type %q", pool.ID, *change.TargetRAIDType)
				}
				if err := validateRAIDDiskCount(*change.TargetRAIDType, len(pool.DiskIDs)+len(change.AddDiskIDs)); err != nil {
					return err
				}
			}
		}
		return nil
	}

	change := request.Volume
	if request.Action == storage.ActionCreate {
		if _, found := topologyPoolByID(topology, change.PoolID); !found {
			return fmt.Errorf("storage pool with stable ID %q does not exist", change.PoolID)
		}
		for _, volume := range topology.Volumes {
			if strings.EqualFold(volume.Name, change.Name) {
				return fmt.Errorf("volume named %q already exists with stable ID %q", change.Name, volume.ID)
			}
		}
		return nil
	}
	volume, found := topologyVolumeByID(topology, change.ID)
	if !found {
		return fmt.Errorf("volume with stable ID %q does not exist", change.ID)
	}
	observedCapacity := volume.AllocatedBytes
	if observedCapacity == 0 {
		observedCapacity = volume.SizeBytes
	}
	if request.Action == storage.ActionUpdate && change.ExpandTo.Mode == storage.CapacityExact && change.ExpandTo.SizeBytes <= observedCapacity {
		return fmt.Errorf("volume expansion exact size %d must exceed observed allocated size %d", change.ExpandTo.SizeBytes, observedCapacity)
	}
	return nil
}

func validateStorageMutationSafety(state storage.State, topology storageTopology, request storage.ChangeRequest) ([]string, uint64, error) {
	if request.Resource == storage.ResourceCache {
		return nil, 0, validateCacheMutationSafety(state, request)
	}
	if request.Resource == storage.ResourceVolume {
		resolved, err := validateVolumeMutationSafety(state, request)
		return nil, resolved, err
	}
	change := request.Pool
	disks := make(map[string]storage.Disk, len(state.Disks))
	for _, disk := range state.Disks {
		disks[strings.TrimSpace(disk.ID)] = disk
	}
	selected := change.DiskIDs
	if request.Action == storage.ActionUpdate {
		selected = change.AddDiskIDs
	}
	for _, diskID := range selected {
		if err := validatePoolCandidateDisk(disks[diskID]); err != nil {
			return nil, 0, fmt.Errorf("stable disk ID %q is not eligible for pool mutation: %w", diskID, err)
		}
	}

	switch request.Action {
	case storage.ActionCreate:
		applicable := applicablePoolRAIDTypes(state.PoolCreation, len(change.DiskIDs))
		if !containsString(applicable, change.RAIDType) {
			return nil, 0, fmt.Errorf("RAID type %q is not applicable to %d selected disks on this DSM model; applicable choices: %s", change.RAIDType, len(change.DiskIDs), strings.Join(applicable, ", "))
		}
		return applicable, 0, nil
	case storage.ActionUpdate:
		pool, _ := topologyPoolByID(topology, change.ID)
		observed, _ := storagePoolByID(state, pool.ID)
		if observed.Actioning {
			return nil, 0, fmt.Errorf("storage pool %q already has an action in progress", pool.ID)
		}
		if !observed.Writable {
			return nil, 0, fmt.Errorf("storage pool %q is not writable", pool.ID)
		}
		if !observed.CanExpand {
			return nil, 0, fmt.Errorf("DSM does not report add-disk expansion available for storage pool %q", pool.ID)
		}
		if observed.MaxDiskCount > 0 && len(pool.DiskIDs)+len(change.AddDiskIDs) > observed.MaxDiskCount {
			return nil, 0, fmt.Errorf("pool expansion would use %d disks but DSM reports a pool limit of %d", len(pool.DiskIDs)+len(change.AddDiskIDs), observed.MaxDiskCount)
		}
	case storage.ActionDelete:
		pool, _ := storagePoolByID(state, change.ID)
		if pool.Actioning {
			return nil, 0, fmt.Errorf("storage pool %q already has an action in progress", pool.ID)
		}
		if !pool.CanDelete {
			return nil, 0, fmt.Errorf("DSM does not report deletion available for storage pool %q", pool.ID)
		}
		if !healthyStorageStatus(pool.Status) || !healthyStorageStatus(pool.Health) {
			return nil, 0, fmt.Errorf("storage pool %q status/health is %q/%q, expected normal or healthy", pool.ID, pool.Status, pool.Health)
		}
	}
	return nil, 0, nil
}

func validateVolumeMutationSafety(state storage.State, request storage.ChangeRequest) (uint64, error) {
	change := request.Volume
	poolID := change.PoolID
	var volume storage.Volume
	if request.Action != storage.ActionCreate {
		var found bool
		volume, found = storageVolumeByID(state, change.ID)
		if !found {
			return 0, fmt.Errorf("volume %q is missing from normalized inventory", change.ID)
		}
		poolID = volume.PoolID
	}
	pool, found := storagePoolByID(state, poolID)
	if !found {
		return 0, fmt.Errorf("parent storage pool %q is missing from normalized inventory", poolID)
	}

	switch request.Action {
	case storage.ActionCreate:
		if !containsString(state.VolumeCreation.SupportedFileSystems, change.FileSystem) {
			return 0, fmt.Errorf("filesystem %q is not advertised by this DSM model; supported choices: %s", change.FileSystem, strings.Join(state.VolumeCreation.SupportedFileSystems, ", "))
		}
		if err := validateVolumePoolForMutation(pool, true); err != nil {
			return 0, err
		}
		if pool.Layout != "single" && pool.Layout != "multiple" {
			return 0, fmt.Errorf("storage pool %q has unsupported or missing volume layout %q", pool.ID, pool.Layout)
		}
		if pool.Layout == "single" {
			if pool.SpacePath == "" {
				return 0, fmt.Errorf("storage pool %q is missing stable DSM space_path", pool.ID)
			}
			if change.Capacity.Mode != storage.CapacityMaximum {
				return 0, fmt.Errorf("single-volume pool %q supports only the explicit maximum capacity policy", pool.ID)
			}
		} else if pool.Path == "" {
			return 0, fmt.Errorf("storage pool %q is missing stable DSM pool_path", pool.ID)
		}
		maximum := pool.AvailableBytes
		if pool.Layout == "single" {
			maximum = pool.SizeBytes
		}
		maximum = minimumPositive(maximum, state.VolumeCreation.MaxFileSystemBytes)
		return resolveVolumeCapacity(*change.Capacity, state.VolumeCreation.MinimumSizeBytes, maximum)

	case storage.ActionUpdate:
		if volume.Actioning {
			return 0, fmt.Errorf("volume %q already has an action in progress", volume.ID)
		}
		if volume.ReadOnly || !volume.Writable {
			return 0, fmt.Errorf("volume %q is not writable", volume.ID)
		}
		if !volume.CanExpand {
			return 0, fmt.Errorf("DSM does not report expansion available for volume %q", volume.ID)
		}
		if !healthyStorageStatus(volume.Status) || !healthyStorageStatus(volume.Health) {
			return 0, fmt.Errorf("volume %q status/health is %q/%q, expected normal or healthy", volume.ID, volume.Status, volume.Health)
		}
		if err := validateVolumePoolForMutation(pool, false); err != nil {
			return 0, err
		}
		if volume.AllocatedBytes == 0 {
			return 0, fmt.Errorf("volume %q is missing its allocated device size", volume.ID)
		}
		if volume.SingleVolume && change.ExpandTo.Mode != storage.CapacityMaximum {
			return 0, fmt.Errorf("single-volume expansion for %q supports only the explicit maximum capacity policy", volume.ID)
		}
		maximum, err := checkedAddBytes(volume.AllocatedBytes, pool.AvailableBytes)
		if err != nil {
			return 0, fmt.Errorf("calculate maximum expansion for volume %q: %w", volume.ID, err)
		}
		maximum = minimumPositive(maximum, volume.MaxFileSystemBytes)
		maximum = minimumPositive(maximum, state.VolumeCreation.MaxFileSystemBytes)
		minimumExpansion, err := checkedAddBytes(volume.AllocatedBytes, 1)
		if err != nil {
			return 0, fmt.Errorf("calculate minimum expansion for volume %q: %w", volume.ID, err)
		}
		resolved, err := resolveVolumeCapacity(*change.ExpandTo, minimumExpansion, maximum)
		if err != nil {
			return 0, err
		}
		if resolved <= volume.AllocatedBytes {
			return 0, fmt.Errorf("volume %q has no allocatable capacity for expansion", volume.ID)
		}
		return resolved, nil

	case storage.ActionDelete:
		if volume.Actioning {
			return 0, fmt.Errorf("volume %q already has an action in progress", volume.ID)
		}
		if !volume.CanDelete {
			return 0, fmt.Errorf("DSM does not report deletion available for volume %q", volume.ID)
		}
		if !healthyStorageStatus(volume.Status) || !healthyStorageStatus(volume.Health) {
			return 0, fmt.Errorf("volume %q status/health is %q/%q, expected normal or healthy", volume.ID, volume.Status, volume.Health)
		}
	}
	return 0, nil
}

func validateVolumePoolForMutation(pool storage.Pool, create bool) error {
	if pool.Actioning {
		return fmt.Errorf("storage pool %q already has an action in progress", pool.ID)
	}
	if !pool.Writable {
		return fmt.Errorf("storage pool %q is not writable", pool.ID)
	}
	if !healthyStorageStatus(pool.Status) || !healthyStorageStatus(pool.Health) {
		return fmt.Errorf("storage pool %q status/health is %q/%q, expected normal or healthy", pool.ID, pool.Status, pool.Health)
	}
	if create {
		if !pool.Compatible {
			return fmt.Errorf("DSM does not report storage pool %q compatible with volume creation", pool.ID)
		}
		if !pool.CanCreateVolume {
			return fmt.Errorf("DSM does not report volume creation available for storage pool %q", pool.ID)
		}
	}
	return nil
}

func resolveVolumeCapacity(policy storage.CapacityPolicy, minimum, maximum uint64) (uint64, error) {
	if minimum == 0 || maximum == 0 {
		return 0, fmt.Errorf("DSM did not report usable minimum/maximum volume capacity")
	}
	maximum = maximum / (uint64(1) << 30) * (uint64(1) << 30)
	if maximum < minimum {
		return 0, fmt.Errorf("maximum allocatable volume capacity %d is below minimum %d", maximum, minimum)
	}
	if policy.Mode == storage.CapacityMaximum {
		return maximum, nil
	}
	if policy.SizeBytes%(uint64(1)<<30) != 0 {
		return 0, fmt.Errorf("exact volume capacity %d must be a whole GiB value", policy.SizeBytes)
	}
	if policy.SizeBytes < minimum {
		return 0, fmt.Errorf("exact volume capacity %d is below minimum %d", policy.SizeBytes, minimum)
	}
	if policy.SizeBytes > maximum {
		return 0, fmt.Errorf("exact volume capacity %d exceeds maximum allocatable capacity %d", policy.SizeBytes, maximum)
	}
	return policy.SizeBytes, nil
}

func minimumPositive(value uint64, limits ...uint64) uint64 {
	for _, limit := range limits {
		if limit > 0 && (value == 0 || limit < value) {
			value = limit
		}
	}
	return value
}

func checkedAddBytes(left, right uint64) (uint64, error) {
	if ^uint64(0)-left < right {
		return 0, fmt.Errorf("byte capacity overflow")
	}
	return left + right, nil
}

func validatePoolCandidateDisk(disk storage.Disk) error {
	if strings.TrimSpace(disk.ID) == "" {
		return fmt.Errorf("disk is missing from normalized inventory")
	}
	usedBy := strings.TrimSpace(disk.UsedBy)
	if disk.InUse || (usedBy != "" && !strings.EqualFold(usedBy, "unused")) {
		return fmt.Errorf("disk is already used by %q", disk.UsedBy)
	}
	if !disk.Selectable {
		return fmt.Errorf("DSM does not report the disk selectable")
	}
	if !healthyStorageStatus(disk.Status) || !healthyStorageStatus(disk.Health) {
		return fmt.Errorf("disk status/health is %q/%q, expected normal or healthy", disk.Status, disk.Health)
	}
	if value := strings.ToLower(strings.TrimSpace(disk.SMARTStatus)); value != "" && value != "normal" && value != "healthy" && value != "passed" && value != "pass" {
		return fmt.Errorf("SMART status is %q", disk.SMARTStatus)
	}
	if value := strings.ToLower(strings.TrimSpace(disk.Compatibility)); value != "support" && value != "supported" && value != "compatible" {
		return fmt.Errorf("drive compatibility is %q", disk.Compatibility)
	}
	return nil
}

func applicablePoolRAIDTypes(constraints storage.PoolCreationConstraints, diskCount int) []string {
	candidates := []string{storage.RAIDBasic, storage.RAID0, storage.RAID1, storage.RAID5, storage.RAID6, storage.RAID10, storage.RAIDJBOD}
	if constraints.SupportsSHR {
		candidates = append([]string{storage.RAIDSHR, storage.RAIDSHR2}, candidates...)
	}
	result := make([]string, 0, len(candidates))
	for _, raidType := range candidates {
		if validateRAIDDiskCount(raidType, diskCount) == nil {
			result = append(result, raidType)
		}
	}
	return result
}

func validateCacheTopologyReferences(topology storageTopology, disks map[string]storageTopologyDisk, occupied map[string]string, request storage.ChangeRequest) error {
	change := request.Cache
	cachedVolumes := make(map[string]string, len(topology.Caches))
	for _, cache := range topology.Caches {
		cachedVolumes[cache.VolumeID] = cache.ID
		for _, diskID := range cache.DiskIDs {
			if _, found := occupied[diskID]; !found {
				occupied[diskID] = cache.ID
			}
		}
	}
	if request.Action == storage.ActionCreate {
		if _, found := topologyVolumeByID(topology, change.VolumeID); !found {
			return fmt.Errorf("volume with stable ID %q does not exist", change.VolumeID)
		}
		if cacheID, found := cachedVolumes[change.VolumeID]; found {
			return fmt.Errorf("volume %q already has SSD cache %q", change.VolumeID, cacheID)
		}
		return validateAvailableDisks(disks, occupied, change.DiskIDs)
	}
	cache, found := topologyCacheByID(topology, change.ID)
	if !found {
		return fmt.Errorf("SSD cache with stable ID %q does not exist", change.ID)
	}
	if len(change.AddDiskIDs) > 0 {
		if err := validateAvailableDisks(disks, occupied, change.AddDiskIDs); err != nil {
			return err
		}
	}
	if request.Action == storage.ActionConvert {
		if cache.CacheType != "" && cache.CacheType == *change.TargetMode {
			return fmt.Errorf("cache %q is already %s", cache.ID, *change.TargetMode)
		}
		if *change.TargetMode == storage.CacheModeReadWrite && change.ProtectionRAID != "" {
			if err := validateRAIDDiskCount(change.ProtectionRAID, len(cache.DiskIDs)+len(change.AddDiskIDs)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateCacheMutationSafety checks live-state safety for an SSD cache change:
// candidate SSD eligibility, parent-volume writability, mode support, and that a
// target cache is not mid-action. Read-write requires the protection RAID
// backend. Unlike a pool disk, a cache SSD may carry the DSM system partition
// (the boot SSDs), so status "sys_partition_normal" is accepted for cache media.
func validateCacheMutationSafety(state storage.State, request storage.ChangeRequest) error {
	change := request.Cache
	if request.Action == storage.ActionCreate {
		volume, found := storageVolumeByID(state, change.VolumeID)
		if !found {
			return fmt.Errorf("parent volume %q is missing from observed state", change.VolumeID)
		}
		if volume.ReadOnly || !volume.Writable {
			return fmt.Errorf("parent volume %q is not writable", change.VolumeID)
		}
		if volume.Actioning {
			return fmt.Errorf("parent volume %q already has an action in progress", change.VolumeID)
		}
		if !healthyStorageStatus(volume.Status) || !healthyStorageStatus(volume.Health) {
			return fmt.Errorf("parent volume %q status/health is %q/%q, expected normal or healthy", change.VolumeID, volume.Status, volume.Health)
		}
		for _, diskID := range change.DiskIDs {
			disk, ok := storageDiskByID(state, diskID)
			if !ok {
				return fmt.Errorf("stable disk ID %q is missing from observed state", diskID)
			}
			if err := validateCacheCandidateDisk(disk); err != nil {
				return fmt.Errorf("stable disk ID %q is not eligible for an SSD cache: %w", diskID, err)
			}
		}
		if state.CacheCreation.MaxDisks > 0 && len(change.DiskIDs) > state.CacheCreation.MaxDisks {
			return fmt.Errorf("cache uses %d SSDs but DSM reports a limit of %d", len(change.DiskIDs), state.CacheCreation.MaxDisks)
		}
		switch change.CacheType {
		case storage.CacheModeReadOnly:
			if !state.CacheCreation.SupportsReadOnly {
				return fmt.Errorf("DSM does not report read-only SSD cache support")
			}
		case storage.CacheModeReadWrite:
			if !state.CacheCreation.SupportsReadWrite || !state.CacheCreation.SupportsProtection {
				return fmt.Errorf("read-write SSD cache requires the RAID protection backend, which is unavailable")
			}
		}
		return nil
	}
	cache, found := storageCacheByID(state, change.ID)
	if !found {
		return fmt.Errorf("SSD cache %q is missing from observed state", change.ID)
	}
	if cache.Actioning || cache.Flushing {
		return fmt.Errorf("SSD cache %q already has an action in progress", change.ID)
	}
	if request.Action == storage.ActionDelete {
		return nil
	}
	// Expand and convert re-add SSDs to an existing cache; their candidate disks
	// must be eligible too.
	for _, diskID := range change.AddDiskIDs {
		disk, ok := storageDiskByID(state, diskID)
		if !ok {
			return fmt.Errorf("stable disk ID %q is missing from observed state", diskID)
		}
		if err := validateCacheCandidateDisk(disk); err != nil {
			return fmt.Errorf("stable disk ID %q is not eligible for an SSD cache: %w", diskID, err)
		}
	}
	if request.Action == storage.ActionConvert && *change.TargetMode == storage.CacheModeReadWrite {
		if !state.CacheCreation.SupportsProtection {
			return fmt.Errorf("read-write SSD cache requires the RAID protection backend, which is unavailable")
		}
	}
	return nil
}

// validateCacheCandidateDisk accepts an SSD that is unused by a storage pool and
// selectable. It deliberately tolerates the "sys_partition_normal" status of the
// DSM boot SSDs, which Synology permits as cache media, while still requiring
// healthy overview status and SMART.
func validateCacheCandidateDisk(disk storage.Disk) error {
	if strings.TrimSpace(disk.ID) == "" {
		return fmt.Errorf("disk is missing from normalized inventory")
	}
	if !strings.EqualFold(strings.TrimSpace(disk.Type), "ssd") {
		return fmt.Errorf("disk media is %q, expected SSD", disk.Type)
	}
	usedBy := strings.TrimSpace(disk.UsedBy)
	if disk.InUse || (usedBy != "" && !strings.EqualFold(usedBy, "unused")) {
		return fmt.Errorf("SSD is already used by %q", disk.UsedBy)
	}
	if !disk.Selectable {
		return fmt.Errorf("DSM does not report the SSD selectable")
	}
	if !healthyStorageStatus(disk.Health) {
		return fmt.Errorf("SSD health is %q, expected normal or healthy", disk.Health)
	}
	if value := strings.ToLower(strings.TrimSpace(disk.SMARTStatus)); value != "" && value != "normal" && value != "healthy" && value != "passed" && value != "pass" {
		return fmt.Errorf("SMART status is %q", disk.SMARTStatus)
	}
	return nil
}

func storageCacheByID(state storage.State, id string) (storage.Cache, bool) {
	for _, cache := range state.Caches {
		if strings.TrimSpace(cache.ID) == id {
			return cache, true
		}
	}
	return storage.Cache{}, false
}

func topologyCacheByID(topology storageTopology, id string) (storageTopologyCache, bool) {
	for _, cache := range topology.Caches {
		if cache.ID == id {
			return cache, true
		}
	}
	return storageTopologyCache{}, false
}

func storageSafetyFingerprint(state storage.State, request storage.ChangeRequest) (string, error) {
	observation := storageSafetyObservation{PoolCreation: state.PoolCreation, VolumeCreation: state.VolumeCreation}
	if request.Resource == storage.ResourceCache && request.Cache != nil {
		cacheCreation := state.CacheCreation
		observation.CacheCreation = &cacheCreation
		change := request.Cache
		selected := change.DiskIDs
		if request.Action != storage.ActionCreate {
			selected = change.AddDiskIDs
		}
		for _, diskID := range selected {
			disk, found := storageDiskByID(state, diskID)
			if !found {
				return "", fmt.Errorf("stable disk ID %q disappeared while creating cache safety fingerprint", diskID)
			}
			observation.Disks = append(observation.Disks, storageSafetyDisk{
				ID: disk.ID, Status: disk.Status, Health: disk.Health, SMARTStatus: disk.SMARTStatus,
				UsedBy: disk.UsedBy, InUse: disk.InUse, Selectable: disk.Selectable, Compatibility: disk.Compatibility,
				Serial: disk.Serial, Firmware: disk.Firmware, SizeBytes: disk.SizeBytes,
			})
		}
		sort.Slice(observation.Disks, func(i, j int) bool { return observation.Disks[i].ID < observation.Disks[j].ID })
		volumeID := change.VolumeID
		if request.Action != storage.ActionCreate {
			cache, found := storageCacheByID(state, change.ID)
			if !found {
				return "", fmt.Errorf("SSD cache %q disappeared while creating safety fingerprint", change.ID)
			}
			volumeID = cache.VolumeID
			diskIDs := append([]string(nil), cache.DiskIDs...)
			sort.Strings(diskIDs)
			observation.Cache = &storageSafetyCache{
				ID: cache.ID, VolumeID: cache.VolumeID, CacheType: cache.CacheType, ProtectionRAID: normalizeObservedRAIDType(cache.ProtectionRAID),
				Status: cache.Status, Health: cache.Health, ProtectionStatus: cache.ProtectionStatus, DiskIDs: diskIDs,
				HasDirtyData: cache.HasDirtyData, Flushing: cache.Flushing, Actioning: cache.Actioning, CanDelete: cache.CanDelete, SizeBytes: cache.SizeBytes,
			}
		}
		if volume, found := storageVolumeByID(state, volumeID); found {
			observation.Volume = &storageSafetyVolume{
				ID: volume.ID, PoolID: volume.PoolID, FileSystem: volume.FileSystem, Status: volume.Status, Health: volume.Health,
				ReadOnly: volume.ReadOnly, Writable: volume.Writable, Actioning: volume.Actioning,
				SingleVolume: volume.SingleVolume, CanExpand: volume.CanExpand, CanDelete: volume.CanDelete,
				AllocatedBytes: volume.AllocatedBytes, MaxFileSystemBytes: volume.MaxFileSystemBytes,
			}
		}
		return fingerprint(observation), nil
	}
	if request.Resource == storage.ResourceVolume && request.Volume != nil {
		poolID := request.Volume.PoolID
		if request.Action != storage.ActionCreate {
			volume, found := storageVolumeByID(state, request.Volume.ID)
			if !found {
				return "", fmt.Errorf("volume %q disappeared while creating safety fingerprint", request.Volume.ID)
			}
			poolID = volume.PoolID
			observation.Volume = &storageSafetyVolume{
				ID: volume.ID, PoolID: volume.PoolID, FileSystem: volume.FileSystem, Status: volume.Status, Health: volume.Health,
				ReadOnly: volume.ReadOnly, Writable: volume.Writable, Actioning: volume.Actioning,
				SingleVolume: volume.SingleVolume, CanExpand: volume.CanExpand, CanDelete: volume.CanDelete,
				AllocatedBytes: volume.AllocatedBytes, MaxFileSystemBytes: volume.MaxFileSystemBytes,
			}
		}
		pool, found := storagePoolByID(state, poolID)
		if !found {
			return "", fmt.Errorf("storage pool %q disappeared while creating volume safety fingerprint", poolID)
		}
		observation.Pool = storageSafetyPoolObservation(pool)
		return fingerprint(observation), nil
	}
	if request.Resource != storage.ResourcePool || request.Pool == nil {
		return fingerprint(observation), nil
	}
	selected := request.Pool.DiskIDs
	if request.Action == storage.ActionUpdate {
		selected = request.Pool.AddDiskIDs
	}
	for _, diskID := range selected {
		disk, found := storageDiskByID(state, diskID)
		if !found {
			return "", fmt.Errorf("stable disk ID %q disappeared while creating safety fingerprint", diskID)
		}
		observation.Disks = append(observation.Disks, storageSafetyDisk{
			ID: disk.ID, Status: disk.Status, Health: disk.Health, SMARTStatus: disk.SMARTStatus,
			UsedBy: disk.UsedBy, InUse: disk.InUse, Selectable: disk.Selectable, Compatibility: disk.Compatibility,
			Serial: disk.Serial, Firmware: disk.Firmware, SizeBytes: disk.SizeBytes,
		})
	}
	sort.Slice(observation.Disks, func(i, j int) bool { return observation.Disks[i].ID < observation.Disks[j].ID })
	if request.Action != storage.ActionCreate {
		pool, found := storagePoolByID(state, request.Pool.ID)
		if !found {
			return "", fmt.Errorf("storage pool %q disappeared while creating safety fingerprint", request.Pool.ID)
		}
		diskIDs := append([]string(nil), pool.DiskIDs...)
		sort.Strings(diskIDs)
		observation.Pool = storageSafetyPoolObservation(pool)
		observation.Pool.DiskIDs = diskIDs
	}
	return fingerprint(observation), nil
}

func storageSafetyPoolObservation(pool storage.Pool) *storageSafetyPool {
	diskIDs := append([]string(nil), pool.DiskIDs...)
	sort.Strings(diskIDs)
	return &storageSafetyPool{
		ID: pool.ID, RAIDType: normalizeObservedRAIDType(pool.RAIDType), Status: pool.Status, Health: pool.Health,
		DiskIDs: diskIDs, Writable: pool.Writable, Actioning: pool.Actioning, CanExpand: pool.CanExpand,
		CanDelete: pool.CanDelete, Compatible: pool.Compatible, CanCreateVolume: pool.CanCreateVolume,
		SizeBytes: pool.SizeBytes, UsedBytes: pool.UsedBytes, AvailableBytes: pool.AvailableBytes,
		Path: pool.Path, SpacePath: pool.SpacePath, Layout: pool.Layout, MaxDiskCount: pool.MaxDiskCount,
	}
}

func storageDiskByID(state storage.State, id string) (storage.Disk, bool) {
	for _, disk := range state.Disks {
		if strings.TrimSpace(disk.ID) == id {
			return disk, true
		}
	}
	return storage.Disk{}, false
}

func storagePoolByID(state storage.State, id string) (storage.Pool, bool) {
	for _, pool := range state.Pools {
		if strings.TrimSpace(pool.ID) == id {
			return pool, true
		}
	}
	return storage.Pool{}, false
}

func storageVolumeByID(state storage.State, id string) (storage.Volume, bool) {
	for _, volume := range state.Volumes {
		if strings.TrimSpace(volume.ID) == id {
			return volume, true
		}
	}
	return storage.Volume{}, false
}

func healthyStorageStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal", "healthy":
		return true
	default:
		return false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func verifyStoragePostcondition(plan StoragePlan, before, after storage.State) (string, error) {
	if plan.Request.Resource == storage.ResourceCache {
		return verifyStorageCachePostcondition(plan, before, after)
	}
	if plan.Request.Resource == storage.ResourceVolume {
		return verifyStorageVolumePostcondition(plan, before, after)
	}
	return verifyStoragePoolPostcondition(plan, before, after)
}

func verifyStorageCachePostcondition(plan StoragePlan, before, after storage.State) (string, error) {
	change := plan.Request.Cache
	switch plan.Request.Action {
	case storage.ActionCreate:
		existing := make(map[string]struct{}, len(before.Caches))
		for _, cache := range before.Caches {
			existing[cache.ID] = struct{}{}
		}
		for _, cache := range after.Caches {
			if _, seen := existing[cache.ID]; seen {
				continue
			}
			if cache.VolumeID != change.VolumeID {
				continue
			}
			if cache.CacheType != "" && change.CacheType != "" && cache.CacheType != change.CacheType {
				return "", fmt.Errorf("new cache on volume %q has mode %q, expected %q", change.VolumeID, cache.CacheType, change.CacheType)
			}
			if failedStorageStatus(cache.Status) {
				return "", fmt.Errorf("new cache on volume %q reports failed status %q", change.VolumeID, cache.Status)
			}
			return cache.ID, nil
		}
		// Some DSM releases re-read the cache asynchronously; accept the volume
		// now reporting a cache even if the array is still settling.
		return "", fmt.Errorf("created SSD cache was not found on volume %q", change.VolumeID)
	case storage.ActionDelete:
		cache, found := storageCacheByID(after, change.ID)
		if !found {
			return change.ID, nil
		}
		// Read-write cache removal flushes dirty data asynchronously; a cache that
		// is still present but actioning, flushing, or no longer in a settled
		// normal state is being torn down and counts as applied.
		if cache.Actioning || cache.Flushing || !healthyStorageStatus(cache.Status) {
			return change.ID, nil
		}
		return "", fmt.Errorf("SSD cache %q still present after removal", change.ID)
	default:
		cache, found := storageCacheByID(after, change.ID)
		if !found {
			return "", fmt.Errorf("SSD cache %q disappeared after %s", change.ID, plan.Request.Action)
		}
		if failedStorageStatus(cache.Status) {
			return "", fmt.Errorf("SSD cache %q reports failed status %q", change.ID, cache.Status)
		}
		return cache.ID, nil
	}
}

func verifyStoragePoolPostcondition(plan StoragePlan, before, after storage.State) (string, error) {
	change := plan.Request.Pool
	switch plan.Request.Action {
	case storage.ActionCreate:
		for _, pool := range after.Pools {
			if pool.Name != change.Name || normalizeObservedRAIDType(pool.RAIDType) != change.RAIDType || !sameStringSet(pool.DiskIDs, change.DiskIDs) {
				continue
			}
			if err := validatePoolPostStatus(pool); err != nil {
				return "", err
			}
			return pool.ID, nil
		}
		return "", fmt.Errorf("created pool was not found with name %q, RAID %q, and stable disks %s", change.Name, change.RAIDType, strings.Join(change.DiskIDs, ", "))
	case storage.ActionUpdate:
		current, found := storagePoolByID(before, change.ID)
		if !found {
			return "", fmt.Errorf("pre-mutation pool %q is missing", change.ID)
		}
		pool, found := storagePoolByID(after, change.ID)
		if !found {
			return "", fmt.Errorf("expanded pool %q disappeared", change.ID)
		}
		expectedDisks := append(append([]string(nil), current.DiskIDs...), change.AddDiskIDs...)
		if !sameStringSet(pool.DiskIDs, expectedDisks) {
			return "", fmt.Errorf("expanded pool %q member disks are %v, expected %v", change.ID, pool.DiskIDs, expectedDisks)
		}
		if normalizeObservedRAIDType(pool.RAIDType) != normalizeObservedRAIDType(current.RAIDType) {
			return "", fmt.Errorf("expanded pool %q RAID changed from %q to %q", change.ID, current.RAIDType, pool.RAIDType)
		}
		if err := validatePoolPostStatus(pool); err != nil {
			return "", err
		}
		return pool.ID, nil
	case storage.ActionDelete:
		if _, found := storagePoolByID(after, change.ID); found {
			return "", fmt.Errorf("deleted pool %q is still present", change.ID)
		}
		return change.ID, nil
	default:
		return "", fmt.Errorf("unsupported storage-pool postcondition action %q", plan.Request.Action)
	}
}

func verifyStorageVolumePostcondition(plan StoragePlan, before, after storage.State) (string, error) {
	change := plan.Request.Volume
	switch plan.Request.Action {
	case storage.ActionCreate:
		beforeIDs := make(map[string]struct{}, len(before.Volumes))
		for _, volume := range before.Volumes {
			beforeIDs[volume.ID] = struct{}{}
		}
		matches := make([]storage.Volume, 0, 1)
		for _, volume := range after.Volumes {
			if _, existed := beforeIDs[volume.ID]; existed {
				continue
			}
			if volume.Name != change.Name || volume.PoolID != change.PoolID || !strings.EqualFold(volume.FileSystem, change.FileSystem) {
				continue
			}
			if plan.References.PoolLayout == "multiple" && volume.AllocatedBytes != plan.ResolvedCapacityBytes {
				continue
			}
			matches = append(matches, volume)
		}
		if len(matches) != 1 {
			return "", fmt.Errorf("expected exactly one new volume matching description %q, pool %q, filesystem %q, and approved capacity; found %d", change.Name, change.PoolID, change.FileSystem, len(matches))
		}
		if err := validateVolumePostStatus(matches[0]); err != nil {
			return "", err
		}
		return matches[0].ID, nil

	case storage.ActionUpdate:
		previous, found := storageVolumeByID(before, change.ID)
		if !found {
			return "", fmt.Errorf("pre-mutation volume %q is missing", change.ID)
		}
		volume, found := storageVolumeByID(after, change.ID)
		if !found {
			return "", fmt.Errorf("expanded volume %q disappeared", change.ID)
		}
		if volume.PoolID != previous.PoolID || !strings.EqualFold(volume.FileSystem, previous.FileSystem) {
			return "", fmt.Errorf("expanded volume %q changed parent pool or filesystem identity", change.ID)
		}
		if volume.SingleVolume {
			if volume.AllocatedBytes <= previous.AllocatedBytes && !volume.Actioning {
				return "", fmt.Errorf("single-volume expansion %q did not increase allocated bytes", change.ID)
			}
		} else if volume.AllocatedBytes < plan.ResolvedCapacityBytes && !volume.Actioning {
			return "", fmt.Errorf("expanded volume %q allocated %d bytes, below approved target %d", change.ID, volume.AllocatedBytes, plan.ResolvedCapacityBytes)
		}
		if err := validateVolumePostStatus(volume); err != nil {
			return "", err
		}
		return volume.ID, nil

	case storage.ActionDelete:
		if _, found := storageVolumeByID(after, change.ID); found {
			return "", fmt.Errorf("deleted volume %q is still present", change.ID)
		}
		return change.ID, nil
	default:
		return "", fmt.Errorf("unsupported volume postcondition action %q", plan.Request.Action)
	}
}

func validatePoolPostStatus(pool storage.Pool) error {
	if healthyStorageStatus(pool.Status) && healthyStorageStatus(pool.Health) {
		return nil
	}
	if pool.Actioning && !failedStorageStatus(pool.Status) && !failedStorageStatus(pool.Health) {
		return nil
	}
	return fmt.Errorf("pool %q post-mutation status/health is %q/%q (actioning=%t)", pool.ID, pool.Status, pool.Health, pool.Actioning)
}

func validateVolumePostStatus(volume storage.Volume) error {
	if healthyStorageStatus(volume.Status) && healthyStorageStatus(volume.Health) {
		return nil
	}
	if volume.Actioning && !failedStorageStatus(volume.Status) && !failedStorageStatus(volume.Health) {
		return nil
	}
	return fmt.Errorf("volume %q post-mutation status/health is %q/%q (actioning=%t)", volume.ID, volume.Status, volume.Health, volume.Actioning)
}

func failedStorageStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "crashed", "broken", "failed", "failure", "unavailable", "critical", "degraded":
		return true
	default:
		return strings.TrimSpace(value) == ""
	}
}

func validateAvailableDisks(disks map[string]storageTopologyDisk, occupied map[string]string, diskIDs []string) error {
	for _, diskID := range diskIDs {
		if _, found := disks[diskID]; !found {
			return fmt.Errorf("unknown stable disk ID %q", diskID)
		}
		if poolID, inUse := occupied[diskID]; inUse {
			return fmt.Errorf("stable disk ID %q already belongs to pool %q", diskID, poolID)
		}
	}
	return nil
}

func storagePreconditionAndReferences(topology storageTopology, request storage.ChangeRequest) (ChangePrecondition, StorageStableReferences) {
	if request.Resource == storage.ResourceCache {
		change := request.Cache
		if request.Action == storage.ActionCreate {
			return ChangePrecondition{ExpectedExists: false}, StorageStableReferences{
				CacheVolumePath: change.VolumeID, DiskIDs: append([]string(nil), change.DiskIDs...),
			}
		}
		cache, _ := topologyCacheByID(topology, change.ID)
		return ChangePrecondition{ExpectedExists: true, ResourceID: cache.ID, Fingerprint: fingerprint(cache)}, StorageStableReferences{
			ResourceID: cache.ID, CacheID: cache.ID, CacheVolumePath: cache.VolumeID,
		}
	}
	if request.Resource == storage.ResourcePool {
		if request.Action == storage.ActionCreate {
			return ChangePrecondition{ExpectedExists: false}, StorageStableReferences{DiskIDs: append([]string(nil), request.Pool.DiskIDs...)}
		}
		pool, _ := topologyPoolByID(topology, request.Pool.ID)
		disks := append([]string(nil), pool.DiskIDs...)
		disks = append(disks, request.Pool.AddDiskIDs...)
		sort.Strings(disks)
		return ChangePrecondition{ExpectedExists: true, ResourceID: pool.ID, Fingerprint: fingerprint(pool)}, StorageStableReferences{
			ResourceID: pool.ID, PoolID: pool.ID, DiskIDs: disks,
		}
	}
	if request.Action == storage.ActionCreate {
		pool, _ := topologyPoolByID(topology, request.Volume.PoolID)
		return ChangePrecondition{ExpectedExists: false}, StorageStableReferences{
			PoolID: pool.ID, PoolPath: pool.Path, SpacePath: pool.SpacePath, PoolLayout: pool.Layout,
		}
	}
	volume, _ := topologyVolumeByID(topology, request.Volume.ID)
	pool, _ := topologyPoolByID(topology, volume.PoolID)
	return ChangePrecondition{ExpectedExists: true, ResourceID: volume.ID, Fingerprint: fingerprint(volume)}, StorageStableReferences{
		ResourceID: volume.ID, PoolID: volume.PoolID, PoolPath: pool.Path, SpacePath: pool.SpacePath, PoolLayout: pool.Layout,
	}
}

func storagePlanEffects(topology storageTopology, request storage.ChangeRequest, resolvedCapacityBytes uint64) (bool, []string, []StorageDestructiveConsequence, []string) {
	warnings := []string{"Backend support is model- and DSM-specific and must be selected per operation before apply."}
	var consequences []StorageDestructiveConsequence
	var summary []string
	destructive := request.Action == storage.ActionDelete
	if request.Resource == storage.ResourceCache {
		change := request.Cache
		destructive = false
		switch request.Action {
		case storage.ActionCreate:
			summary = []string{fmt.Sprintf("Create a %s SSD cache on volume %s using stable SSDs %s.", change.CacheType, change.VolumeID, strings.Join(change.DiskIDs, ", "))}
			if change.CacheType == storage.CacheModeReadWrite {
				warnings = append(warnings, "A read-write SSD cache holds dirty data; a later removal flushes it before teardown.")
			} else {
				warnings = append(warnings, "A read-only SSD cache can be removed live without data loss.")
			}
		case storage.ActionUpdate:
			summary = []string{fmt.Sprintf("Add stable SSDs %s to SSD cache %s.", strings.Join(change.AddDiskIDs, ", "), change.ID)}
			warnings = append(warnings, "Adding SSDs can start an SSD cache rebuild.")
		case storage.ActionConvert:
			summary = []string{fmt.Sprintf("Convert SSD cache %s to %s.", change.ID, *change.TargetMode)}
			if *change.TargetMode == storage.CacheModeReadOnly {
				destructive = true
				warnings = append(warnings, "Converting a read-write cache to read-only flushes all dirty cache data before teardown.")
				consequences = append(consequences, StorageDestructiveConsequence{
					Kind: "cache_dirty_flush", ResourceIDs: []string{change.ID},
					Description: "Dirty read-write cache data is flushed during conversion; interrupting it risks data loss.",
				})
			}
		case storage.ActionDelete:
			cache, _ := topologyCacheByID(topology, change.ID)
			resourceIDs := []string{cache.ID}
			if cache.VolumeID != "" {
				resourceIDs = append(resourceIDs, cache.VolumeID)
				sort.Strings(resourceIDs)
			}
			if cache.CacheType == storage.CacheModeReadWrite {
				destructive = true
				warnings = append(warnings, "Removing a read-write SSD cache flushes dirty data; interrupting it risks data loss.")
				consequences = append(consequences, StorageDestructiveConsequence{
					Kind: "cache_dirty_flush", ResourceIDs: resourceIDs,
					Description: "Dirty read-write cache data is flushed during removal; interrupting it risks data loss.",
				})
			} else {
				warnings = append(warnings, "Removing a read-only SSD cache is live and non-destructive.")
			}
			summary = []string{fmt.Sprintf("Remove SSD cache %s from volume %s.", cache.ID, cache.VolumeID)}
		}
		return destructive, warnings, consequences, summary
	}
	if request.Resource == storage.ResourcePool {
		change := request.Pool
		switch request.Action {
		case storage.ActionCreate:
			summary = []string{fmt.Sprintf("Create storage pool %q as %s using stable disks %s.", change.Name, change.RAIDType, strings.Join(change.DiskIDs, ", "))}
			warnings = append(warnings, "DSM drive checking is enabled for pool creation and may run for an extended period.")
		case storage.ActionUpdate:
			if len(change.AddDiskIDs) > 0 {
				summary = append(summary, fmt.Sprintf("Add stable disks %s to storage pool %s.", strings.Join(change.AddDiskIDs, ", "), change.ID))
				warnings = append(warnings, "Adding disks can start a long-running RAID reshape or rebuild and cannot be represented as a field reset.")
			}
			if change.TargetRAIDType != nil {
				destructive = true
				summary = append(summary, fmt.Sprintf("Migrate storage pool %s to RAID type %s.", change.ID, *change.TargetRAIDType))
				warnings = append(warnings, "RAID migration can rebuild or replace the existing topology; verify the selected backend supports this exact transition.")
				consequences = append(consequences, StorageDestructiveConsequence{
					Kind: "raid_topology_replacement", ResourceIDs: []string{change.ID},
					Description: "The existing pool topology may be replaced during RAID migration, risking data loss if interrupted or unsupported.",
				})
			}
		case storage.ActionDelete:
			pool, _ := topologyPoolByID(topology, change.ID)
			resourceIDs := []string{pool.ID}
			for _, volume := range topology.Volumes {
				if volume.PoolID == pool.ID {
					resourceIDs = append(resourceIDs, volume.ID)
				}
			}
			sort.Strings(resourceIDs)
			warnings = append(warnings, "Deleting a storage pool permanently destroys the pool and every volume it contains.")
			consequences = append(consequences, StorageDestructiveConsequence{
				Kind: "permanent_data_loss", ResourceIDs: resourceIDs,
				Description: "All data in the storage pool and its child volumes will be permanently deleted.",
			})
			summary = []string{fmt.Sprintf("Delete storage pool %s and its %d child volume(s).", pool.ID, len(resourceIDs)-1)}
		}
	} else {
		change := request.Volume
		switch request.Action {
		case storage.ActionCreate:
			summary = []string{fmt.Sprintf("Create %s volume %q in storage pool %s with %s capacity policy resolved to %d bytes.", change.FileSystem, change.Name, change.PoolID, change.Capacity.Mode, resolvedCapacityBytes)}
		case storage.ActionUpdate:
			summary = []string{fmt.Sprintf("Expand volume %s using the %s capacity policy resolved to %d total allocated bytes.", change.ID, change.ExpandTo.Mode, resolvedCapacityBytes)}
			warnings = append(warnings, "Volume expansion is additive; shrinking is deliberately not expressible by this contract.")
		case storage.ActionDelete:
			volume, _ := topologyVolumeByID(topology, change.ID)
			warnings = append(warnings, "Deleting a volume permanently destroys all data stored on that volume.")
			consequences = append(consequences, StorageDestructiveConsequence{
				Kind: "permanent_data_loss", ResourceIDs: []string{volume.ID},
				Description: "All data in the volume will be permanently deleted.",
			})
			summary = []string{fmt.Sprintf("Delete volume %s from storage pool %s.", volume.ID, volume.PoolID)}
		}
	}
	return destructive, warnings, consequences, summary
}

func validateStoragePlan(plan StoragePlan, approvalHash string) error {
	if plan.APIVersion != managementAPIVersion {
		return fmt.Errorf("unsupported storage plan API version %q", plan.APIVersion)
	}
	canonical, err := canonicalStorageRequest(plan.Request)
	if err != nil {
		return err
	}
	if fingerprint(canonical) != fingerprint(plan.Request) {
		return fmt.Errorf("storage plan request is not canonical")
	}
	expected, err := storagePlanHash(plan)
	if err != nil {
		return err
	}
	if plan.Hash == "" || plan.Hash != expected {
		return fmt.Errorf("storage plan hash is invalid")
	}
	if approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match storage plan hash")
	}
	return nil
}

func storagePlanHash(plan StoragePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func canonicalRAIDType(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	switch normalized {
	case storage.RAIDSHR, storage.RAIDSHR2, storage.RAID0, storage.RAID1, storage.RAID5, storage.RAID6, storage.RAID10, storage.RAIDJBOD, storage.RAIDBasic:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported RAID type %q", value)
	}
}

func normalizeObservedRAIDType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "shr_without_disk_protect", "shr_with_1_disk_protect", "shr_1":
		return storage.RAIDSHR
	case "shr_with_2_disk_protect", "shr_2":
		return storage.RAIDSHR2
	case "raid_linear":
		return storage.RAIDJBOD
	}
	normalized, err := canonicalRAIDType(value)
	if err == nil {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func validateRAIDDiskCount(raidType string, count int) error {
	valid := false
	switch raidType {
	case storage.RAIDSHR:
		valid = count >= 1
	case storage.RAIDSHR2, storage.RAID6:
		valid = count >= 4
	case storage.RAID0, storage.RAIDJBOD:
		valid = count >= 2
	case storage.RAID1:
		valid = count >= 2 && count <= 4
	case storage.RAID5:
		valid = count >= 3
	case storage.RAID10:
		valid = count >= 4 && count%2 == 0
	case storage.RAIDBasic:
		valid = count == 1
	}
	if !valid {
		return fmt.Errorf("RAID type %q has invalid disk count %d", raidType, count)
	}
	return nil
}

func canonicalIDs(label string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s ID cannot be empty", label)
		}
		result[index] = value
	}
	sort.Strings(result)
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, fmt.Errorf("duplicate stable %s ID %q", label, result[index])
		}
	}
	return result, nil
}

func validateStorageName(resource, name string) error {
	if name == "" {
		return fmt.Errorf("%s create requires name", resource)
	}
	if len(name) > 128 {
		return fmt.Errorf("%s name is longer than 128 bytes", resource)
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%s name contains a control character", resource)
		}
	}
	return nil
}

func topologyPoolByID(topology storageTopology, id string) (storageTopologyPool, bool) {
	for _, pool := range topology.Pools {
		if pool.ID == id {
			return pool, true
		}
	}
	return storageTopologyPool{}, false
}

func topologyVolumeByID(topology storageTopology, id string) (storageTopologyVolume, bool) {
	for _, volume := range topology.Volumes {
		if volume.ID == id {
			return volume, true
		}
	}
	return storageTopologyVolume{}, false
}
