package contract

import (
	"context"
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove/stream"
)

type streamRow struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	State     string `json:"state"`
	Offset    int64  `json:"offset"`
	TotalSize *int64 `json:"totalSize"`
}

type streamsListOutput struct {
	Streams []streamRow `json:"streams"`
	Active  int         `json:"active"`
	Max     int         `json:"max"`
}

// streamsListHandler lists the streams open in this process's pool. They
// are not persisted: a restart loses every one, and none can be resumed.
func streamsListHandler(deps Deps) func(context.Context, storeInput, contract.Principal) (streamsListOutput, error) {
	return func(_ context.Context, in storeInput, _ contract.Principal) (streamsListOutput, error) {
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return streamsListOutput{}, err
		}
		out := streamsListOutput{Streams: []streamRow{}, Max: st.Trove.Config().PoolSize}
		pool := st.Trove.Pool()
		if pool == nil {
			return out, nil
		}
		pool.Range(func(s *stream.Stream) bool {
			row := streamRow{
				ID: s.ID.String(), Direction: s.Direction.String(), Bucket: s.Bucket, Key: s.Key,
				State: string(s.State()), Offset: s.Offset(),
			}
			if total := s.TotalSize(); total >= 0 {
				row.TotalSize = &total
			}
			out.Streams = append(out.Streams, row)
			return true
		})
		sort.Slice(out.Streams, func(i, j int) bool { return out.Streams[i].ID < out.Streams[j].ID })
		out.Active = pool.ActiveCount()
		return out, nil
	}
}
