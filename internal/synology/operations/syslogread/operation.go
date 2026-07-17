// Package syslogread implements the read-only DSM system log (Log Center) list
// operation. It calls SYNO.Core.SyslogClient.Log.list and never mutates DSM.
package syslogread

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName        = "SYNO.Core.SyslogClient.Log"
	CapabilityName = "log.read"
	OperationName  = "log.list"
)

// Input carries the DSM-applied filters. Level is intentionally absent: DSM has
// no stable server-side severity filter, so severity is filtered by the caller.
type Input struct {
	Limit    int
	Offset   int
	Keyword  string
	LogType  string
	DateFrom int64
	DateTo   int64
}

var operation = compatibility.Operation[Input, syslog.State]{
	Name: OperationName,
	Variants: []compatibility.Variant[Input, syslog.State]{
		{
			Name: "core-syslogclient-log-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input Input) (syslog.State, error) {
				parameters := map[string]any{"start": input.Offset, "limit": input.Limit}
				if input.Keyword != "" {
					parameters["keyword"] = input.Keyword
				}
				if input.LogType != "" {
					parameters["logtype"] = input.LogType
				}
				// DSM filters by time only when date_from is present; date_to is
				// an optional inclusive upper bound.
				if input.DateFrom > 0 {
					parameters["date_from"] = input.DateFrom
					if input.DateTo > 0 {
						parameters["date_to"] = input.DateTo
					}
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "list", JSONParameters: parameters})
				if err != nil {
					return syslog.State{}, fmt.Errorf("call %s.list v1: %w", APIName, err)
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

func Execute(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input Input) (syslog.State, compatibility.Selection, error) {
	return operation.Run(ctx, target, executor, input)
}
