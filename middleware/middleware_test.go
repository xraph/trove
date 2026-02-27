package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirection_String(t *testing.T) {
	tests := []struct {
		dir  Direction
		want string
	}{
		{DirectionRead, "read"},
		{DirectionWrite, "write"},
		{DirectionReadWrite, "readwrite"},
		{Direction(0), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.dir.String())
		})
	}
}

func TestDirection_Bitmask(t *testing.T) {
	assert.Equal(t, DirectionReadWrite, DirectionRead|DirectionWrite)
	assert.True(t, DirectionReadWrite&DirectionRead != 0)
	assert.True(t, DirectionReadWrite&DirectionWrite != 0)
	assert.True(t, DirectionRead&DirectionWrite == 0)
}
