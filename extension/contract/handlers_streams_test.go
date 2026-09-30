package contract

import (
	"context"
	"testing"

	"github.com/xraph/trove/stream"
)

func TestStreamsList(t *testing.T) {
	tv := openMem(t)
	deps := testDeps(t, newStores(tv))
	ctx := context.Background()

	empty, err := streamsListHandler(deps)(ctx, storeInput{}, principalFor("u"))
	if err != nil || empty.Streams == nil || len(empty.Streams) != 0 || empty.Max != tv.Config().PoolSize {
		t.Fatalf("empty = %+v, %v", empty, err)
	}

	s, err := tv.Stream(ctx, "data", "big.bin", stream.DirectionUpload)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer tv.Pool().Release(s)
	s.SetTotalSize(2048)

	out, err := streamsListHandler(deps)(ctx, storeInput{}, principalFor("u"))
	if err != nil || len(out.Streams) != 1 || out.Active != 1 {
		t.Fatalf("one stream = %+v, %v", out, err)
	}
	row := out.Streams[0]
	if row.Direction != "upload" || row.Bucket != "data" || row.Key != "big.bin" || row.TotalSize == nil || *row.TotalSize != 2048 || row.ID == "" {
		t.Fatalf("row = %+v", row)
	}
}
