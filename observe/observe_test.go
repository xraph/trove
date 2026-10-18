package observe_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/xraph/trove/observe"
)

func TestMetricNames(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range observe.Metrics() {
		assert.False(t, seen[m.Name], "%s declared twice", m.Name)
		seen[m.Name] = true
		assert.True(t, strings.HasPrefix(m.Name, "trove_"), m.Name)
		assert.NotEmpty(t, m.Help, m.Name)
		switch m.Kind {
		case observe.KindCounter:
			assert.True(t, strings.HasSuffix(m.Name, "_total"), m.Name)
			assert.Nil(t, m.Buckets, m.Name)
		case observe.KindHistogram:
			assert.True(t, strings.HasSuffix(m.Name, "_seconds"), m.Name)
			assert.Equal(t, "s", m.Unit, m.Name)
			assert.NotEmpty(t, m.Buckets, m.Name)
		default:
			t.Errorf("%s has kind %v", m.Name, m.Kind)
		}
	}
	assert.Len(t, seen, 6)
}

func TestMetricsReturnsCopies(t *testing.T) {
	a := observe.Metrics()
	a[0].Labels[0] = "changed"
	a[0].Buckets[0] = -1
	b := observe.Metrics()
	assert.Equal(t, observe.LabelDriver, b[0].Labels[0])
	assert.InDelta(t, observe.OperationBuckets[0], b[0].Buckets[0], 0)
}
