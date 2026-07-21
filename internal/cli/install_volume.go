package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/storage"
)

// createAllDiskVolume builds a NAS's first usable storage as the closing step of
// the install one-shot: it reads the disk inventory, creates one storage pool of
// the requested RAID type over EVERY disk, re-reads to learn the new pool's
// stable id, then creates a single volume of the requested filesystem that fills
// the pool. Pool and volume are two separate guarded plan/apply cycles (a plan
// carries exactly one resource, and the volume needs the pool's stable id, which
// only exists after the pool is created).
//
// Both cycles self-approve with the plan hash the planner just computed. That is
// the intended behaviour here: creating a pool is destructive, and the operator
// opted into it explicitly with --create-volume; there is no separate human plan
// review in this unattended one-shot. A NAS that already has a pool is left
// untouched.
func createAllDiskVolume(cmd *cobra.Command, service *application.Service, nas, raidType, filesystem string, allowUnsupported bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	before, err := service.GetStorageState(ctx, nas)
	if err != nil {
		return fmt.Errorf("read storage inventory: %w", err)
	}
	if len(before.Storage.Pools) > 0 || len(before.Storage.Volumes) > 0 {
		fmt.Fprintln(out, "Storage already has a pool/volume; leaving it unchanged.")
		return nil
	}
	diskIDs := make([]string, 0, len(before.Storage.Disks))
	for _, disk := range before.Storage.Disks {
		if id := strings.TrimSpace(disk.ID); id != "" {
			diskIDs = append(diskIDs, id)
		}
	}
	if len(diskIDs) == 0 {
		return errors.New("no disks reported by the NAS; cannot create a volume")
	}

	fmt.Fprintf(out, "Creating a %s storage pool over %d disk(s): %s ...\n", raidType, len(diskIDs), strings.Join(diskIDs, ", "))
	poolReq := storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourcePool,
		Pool: &storage.PoolChange{Name: "pool1", RAIDType: raidType, DiskIDs: diskIDs, AllowUnsupportedDisks: allowUnsupported},
	}
	if err := planAndApplyStorage(ctx, service, nas, poolReq); err != nil {
		return fmt.Errorf("create storage pool: %w", err)
	}

	after, err := service.GetStorageState(ctx, nas)
	if err != nil {
		return fmt.Errorf("re-read storage after pool creation: %w", err)
	}
	poolID := newlyCreatedPoolID(before.Storage, after.Storage)
	if poolID == "" {
		return errors.New("the storage pool was created but could not be identified for volume creation; create the volume with 'dsmctl storage'")
	}

	fmt.Fprintf(out, "Creating a %s volume filling pool %q ...\n", filesystem, poolID)
	volumeReq := storage.ChangeRequest{
		Action: storage.ActionCreate, Resource: storage.ResourceVolume,
		Volume: &storage.VolumeChange{Name: "volume1", PoolID: poolID, FileSystem: filesystem, Capacity: &storage.CapacityPolicy{Mode: storage.CapacityMaximum}},
	}
	if err := planAndApplyStorage(ctx, service, nas, volumeReq); err != nil {
		return fmt.Errorf("create volume: %w", err)
	}
	fmt.Fprintf(out, "Storage ready: a %s %s volume now exists at /volume1 (a fresh RAID runs a background parity pass for a few hours; it is usable now).\n", raidType, filesystem)
	return nil
}

// planAndApplyStorage plans a guarded storage change and immediately applies it,
// self-approving with the plan's own hash. Callers gate the destructive intent.
func planAndApplyStorage(ctx context.Context, service *application.Service, nas string, request storage.ChangeRequest) error {
	plan, err := service.PlanStorageChange(ctx, nas, request)
	if err != nil {
		return err
	}
	if _, err := service.ApplyStoragePlan(ctx, plan, plan.Hash); err != nil {
		return err
	}
	return nil
}

// newlyCreatedPoolID returns the stable id of the pool present in after but not
// in before. On a fresh NAS (no pools before) it returns the sole new pool.
func newlyCreatedPoolID(before, after storage.State) string {
	existed := make(map[string]bool, len(before.Pools))
	for _, pool := range before.Pools {
		existed[strings.TrimSpace(pool.ID)] = true
	}
	for _, pool := range after.Pools {
		if id := strings.TrimSpace(pool.ID); id != "" && !existed[id] {
			return id
		}
	}
	return ""
}
