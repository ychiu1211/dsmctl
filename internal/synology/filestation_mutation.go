package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	filestationops "github.com/ychiu1211/dsmctl/internal/synology/operations/filestation"
)

type FileStationChangeRequest = filestation.ChangeRequest
type FileStationMutationResult = filestationops.MutationResult
type FileStationFavorites = filestation.Favorites
type FileStationSharingLinks = filestation.SharingLinks
type FileStationBackgroundTasks = filestation.BackgroundTasks

// allFileStationAPINames is every FileStation API (reads, transfer, mutations)
// so a mutation or capability call discovers the full surface in one round trip.
func allFileStationAPINames() []string {
	return append(filestationops.APINames(), filestationops.MutationAPINames()...)
}

// ApplyFileStationChange performs one FileStation mutation and returns its
// normalized result. Upload is not handled here — it streams local bytes and is
// performed by UploadFile from the application layer. password is the resolved
// archive password for compress/extract and is ignored otherwise; it never
// enters a plan or a log.
func (c *Client) ApplyFileStationChange(ctx context.Context, request FileStationChangeRequest, password string) (FileStationMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return FileStationMutationResult{}, fmt.Errorf("prepare FileStation mutation target: %w", err)
	}
	executor := lockedExecutor{client: c}
	switch request.Action {
	case filestation.ActionCreateFolder:
		change := request.CreateFolder
		result, _, err := filestationops.ExecuteCreateFolder(ctx, c.target, executor, filestationops.CreateFolderInput{
			Parent: change.Parent, Name: change.Name, CreateParents: change.CreateParents,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.CreateFolderCapabilityName)
		return result, nil
	case filestation.ActionRename:
		change := request.Rename
		result, _, err := filestationops.ExecuteRename(ctx, c.target, executor, filestationops.RenameInput{Path: change.Path, NewName: change.NewName})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.RenameCapabilityName)
		return result, nil
	case filestation.ActionCopy, filestation.ActionMove:
		change := request.Transfer
		result, _, err := filestationops.ExecuteCopyMove(ctx, c.target, executor, filestationops.TransferInput{
			Sources: change.Sources, DestFolder: change.DestFolder, Overwrite: change.Overwrite, Move: request.Action == filestation.ActionMove,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.CopyMoveCapabilityName)
		return result, nil
	case filestation.ActionDelete:
		result, _, err := filestationops.ExecuteDelete(ctx, c.target, executor, filestationops.DeleteInput{Paths: request.Delete.Paths})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.DeleteCapabilityName)
		return result, nil
	case filestation.ActionCompress:
		change := request.Compress
		result, _, err := filestationops.ExecuteCompress(ctx, c.target, executor, filestationops.CompressInput{
			Sources: change.Sources, DestArchive: change.DestArchive, Format: change.Format, Level: change.Level, Password: password,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.CompressCapabilityName)
		return result, nil
	case filestation.ActionExtract:
		change := request.Extract
		result, _, err := filestationops.ExecuteExtract(ctx, c.target, executor, filestationops.ExtractInput{
			Archive: change.Archive, DestFolder: change.DestFolder, Overwrite: change.Overwrite, Password: password,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.ExtractCapabilityName)
		return result, nil
	case filestation.ActionShareLinkCreate:
		change := request.ShareLink
		result, _, err := filestationops.ExecuteSharingCreate(ctx, c.target, executor, filestationops.SharingCreateInput{
			Path: change.Path, Password: password, ExpireDate: change.ExpireDate,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.SharingCapabilityName)
		return result, nil
	case filestation.ActionShareLinkDelete:
		result, _, err := filestationops.ExecuteSharingDelete(ctx, c.target, executor, filestationops.SharingDeleteInput{LinkID: request.ShareLink.LinkID})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.SharingCapabilityName)
		return result, nil
	case filestation.ActionShareLinkEdit:
		change := request.ShareLink
		result, _, err := filestationops.ExecuteSharingEdit(ctx, c.target, executor, filestationops.SharingEditInput{
			LinkID: change.LinkID, Password: password, ExpireDate: change.ExpireDate,
		})
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.SharingCapabilityName)
		return result, nil
	case filestation.ActionShareLinkClearInvalid:
		result, _, err := filestationops.ExecuteSharingClearInvalid(ctx, c.target, executor)
		if err != nil {
			return FileStationMutationResult{}, err
		}
		c.target.AddCapability(filestationops.SharingCapabilityName)
		return result, nil
	default:
		return FileStationMutationResult{}, fmt.Errorf("unsupported FileStation mutation action %q", request.Action)
	}
}

// FileStationSharingList reads the public sharing links.
func (c *Client) FileStationSharingList(ctx context.Context) (FileStationSharingLinks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return FileStationSharingLinks{}, fmt.Errorf("prepare FileStation sharing target: %w", err)
	}
	links, _, err := filestationops.ExecuteSharingList(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FileStationSharingLinks{}, fmt.Errorf("list FileStation sharing links: %w", err)
	}
	c.target.AddCapability(filestationops.SharingCapabilityName)
	return links, nil
}

// FileStationBackgroundTasks reads the background file-operation task list.
func (c *Client) FileStationBackgroundTasks(ctx context.Context) (FileStationBackgroundTasks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return FileStationBackgroundTasks{}, fmt.Errorf("prepare FileStation background task target: %w", err)
	}
	tasks, _, err := filestationops.ExecuteBackgroundTaskList(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FileStationBackgroundTasks{}, fmt.Errorf("list FileStation background tasks: %w", err)
	}
	c.target.AddCapability(filestationops.BackgroundTaskCapabilityName)
	return tasks, nil
}

// FileStationFavoriteList reads the current session's personal favorites.
func (c *Client) FileStationFavoriteList(ctx context.Context) (FileStationFavorites, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return FileStationFavorites{}, fmt.Errorf("prepare FileStation favorites target: %w", err)
	}
	favorites, _, err := filestationops.ExecuteFavoriteList(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FileStationFavorites{}, fmt.Errorf("list FileStation favorites: %w", err)
	}
	c.target.AddCapability(filestationops.FavoriteCapabilityName)
	return favorites, nil
}

// FileStationFavoriteAdd adds a personal favorite pointing at path.
func (c *Client) FileStationFavoriteAdd(ctx context.Context, path, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return fmt.Errorf("prepare FileStation favorites target: %w", err)
	}
	if _, err := filestationops.ExecuteFavoriteAdd(ctx, c.target, lockedExecutor{client: c}, filestationops.FavoriteAddInput{Path: path, Name: name}); err != nil {
		return fmt.Errorf("add FileStation favorite %q: %w", path, err)
	}
	c.target.AddCapability(filestationops.FavoriteCapabilityName)
	return nil
}

// FileStationFavoriteDelete removes the personal favorite pointing at path.
func (c *Client) FileStationFavoriteDelete(ctx context.Context, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return fmt.Errorf("prepare FileStation favorites target: %w", err)
	}
	if _, err := filestationops.ExecuteFavoriteDelete(ctx, c.target, lockedExecutor{client: c}, filestationops.FavoriteDeleteInput{Path: path}); err != nil {
		return fmt.Errorf("delete FileStation favorite %q: %w", path, err)
	}
	c.target.AddCapability(filestationops.FavoriteCapabilityName)
	return nil
}
