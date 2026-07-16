package storageinventory

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName        = "SYNO.Storage.CGI.Storage"
	CapabilityName = "storage.inventory"
	OperationName  = "storage.inventory"
)

type Input struct{}

var operation = compatibility.Operation[Input, storage.State]{
	Name: OperationName,
	Variants: []compatibility.Variant[Input, storage.State]{
		{
			Name:     "storage-cgi-v1",
			API:      APIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (storage.State, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API:     APIName,
					Version: 1,
					Method:  "load_info",
				})
				if err != nil {
					return storage.State{}, fmt.Errorf("call %s.load_info v1: %w", APIName, err)
				}
				return decode(data)
			},
		},
	},
}

func APINames() []string {
	return operation.APINames()
}

func Select(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := operation.Select(target)
	return selection, err
}

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (storage.State, compatibility.Selection, error) {
	return operation.Run(ctx, target, executor, Input{})
}
