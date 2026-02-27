package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/id"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		prefix id.Prefix
	}{
		{"Object", id.PrefixObject},
		{"Bucket", id.PrefixBucket},
		{"UploadSession", id.PrefixUploadSession},
		{"DownloadSession", id.PrefixDownloadSession},
		{"Stream", id.PrefixStream},
		{"Pool", id.PrefixPool},
		{"Version", id.PrefixVersion},
		{"Chunk", id.PrefixChunk},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := id.New(tt.prefix)
			assert.False(t, got.IsNil())
			assert.Equal(t, tt.prefix, got.Prefix())
			assert.NotEmpty(t, got.String())
		})
	}
}

func TestConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() id.ID
		expected id.Prefix
	}{
		{"NewObjectID", id.NewObjectID, id.PrefixObject},
		{"NewBucketID", id.NewBucketID, id.PrefixBucket},
		{"NewUploadSessionID", id.NewUploadSessionID, id.PrefixUploadSession},
		{"NewDownloadSessionID", id.NewDownloadSessionID, id.PrefixDownloadSession},
		{"NewStreamID", id.NewStreamID, id.PrefixStream},
		{"NewPoolID", id.NewPoolID, id.PrefixPool},
		{"NewVersionID", id.NewVersionID, id.PrefixVersion},
		{"NewChunkID", id.NewChunkID, id.PrefixChunk},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			assert.False(t, got.IsNil())
			assert.Equal(t, tt.expected, got.Prefix())
		})
	}
}

func TestParse(t *testing.T) {
	t.Run("valid ID", func(t *testing.T) {
		original := id.NewObjectID()
		parsed, err := id.Parse(original.String())
		require.NoError(t, err)
		assert.Equal(t, original.String(), parsed.String())
		assert.Equal(t, id.PrefixObject, parsed.Prefix())
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := id.Parse("")
		assert.Error(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := id.Parse("not-a-valid-typeid")
		assert.Error(t, err)
	})
}

func TestParseWithPrefix(t *testing.T) {
	t.Run("matching prefix", func(t *testing.T) {
		original := id.NewBucketID()
		parsed, err := id.ParseWithPrefix(original.String(), id.PrefixBucket)
		require.NoError(t, err)
		assert.Equal(t, original.String(), parsed.String())
	})

	t.Run("mismatched prefix", func(t *testing.T) {
		original := id.NewBucketID()
		_, err := id.ParseWithPrefix(original.String(), id.PrefixObject)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected prefix")
	})
}

func TestConvenienceParsers(t *testing.T) {
	tests := []struct {
		name   string
		gen    func() id.ID
		parser func(string) (id.ID, error)
	}{
		{"ParseObjectID", id.NewObjectID, id.ParseObjectID},
		{"ParseBucketID", id.NewBucketID, id.ParseBucketID},
		{"ParseUploadSessionID", id.NewUploadSessionID, id.ParseUploadSessionID},
		{"ParseDownloadSessionID", id.NewDownloadSessionID, id.ParseDownloadSessionID},
		{"ParseStreamID", id.NewStreamID, id.ParseStreamID},
		{"ParsePoolID", id.NewPoolID, id.ParsePoolID},
		{"ParseVersionID", id.NewVersionID, id.ParseVersionID},
		{"ParseChunkID", id.NewChunkID, id.ParseChunkID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.gen()
			parsed, err := tt.parser(original.String())
			require.NoError(t, err)
			assert.Equal(t, original.String(), parsed.String())
		})
	}
}

func TestMustParse(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		original := id.NewObjectID()
		assert.NotPanics(t, func() {
			parsed := id.MustParse(original.String())
			assert.Equal(t, original.String(), parsed.String())
		})
	})

	t.Run("invalid panics", func(t *testing.T) {
		assert.Panics(t, func() {
			id.MustParse("invalid")
		})
	})
}

func TestMustParseWithPrefix(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		original := id.NewObjectID()
		assert.NotPanics(t, func() {
			parsed := id.MustParseWithPrefix(original.String(), id.PrefixObject)
			assert.Equal(t, original.String(), parsed.String())
		})
	})

	t.Run("wrong prefix panics", func(t *testing.T) {
		original := id.NewObjectID()
		assert.Panics(t, func() {
			id.MustParseWithPrefix(original.String(), id.PrefixBucket)
		})
	})
}

func TestNilID(t *testing.T) {
	assert.True(t, id.Nil.IsNil())
	assert.Empty(t, id.Nil.String())
	assert.Equal(t, id.Prefix(""), id.Nil.Prefix())
}

func TestMarshalText(t *testing.T) {
	t.Run("valid ID", func(t *testing.T) {
		original := id.NewObjectID()
		data, err := original.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, original.String(), string(data))
	})

	t.Run("nil ID", func(t *testing.T) {
		data, err := id.Nil.MarshalText()
		require.NoError(t, err)
		assert.Empty(t, data)
	})
}

func TestUnmarshalText(t *testing.T) {
	t.Run("valid ID", func(t *testing.T) {
		original := id.NewObjectID()
		var parsed id.ID
		err := parsed.UnmarshalText([]byte(original.String()))
		require.NoError(t, err)
		assert.Equal(t, original.String(), parsed.String())
	})

	t.Run("empty data", func(t *testing.T) {
		var parsed id.ID
		err := parsed.UnmarshalText([]byte{})
		require.NoError(t, err)
		assert.True(t, parsed.IsNil())
	})
}

func TestValue(t *testing.T) {
	t.Run("valid ID", func(t *testing.T) {
		original := id.NewObjectID()
		val, err := original.Value()
		require.NoError(t, err)
		assert.Equal(t, original.String(), val)
	})

	t.Run("nil ID", func(t *testing.T) {
		val, err := id.Nil.Value()
		require.NoError(t, err)
		assert.Nil(t, val)
	})
}

func TestScan(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		original := id.NewObjectID()
		var scanned id.ID
		err := scanned.Scan(original.String())
		require.NoError(t, err)
		assert.Equal(t, original.String(), scanned.String())
	})

	t.Run("byte slice value", func(t *testing.T) {
		original := id.NewObjectID()
		var scanned id.ID
		err := scanned.Scan([]byte(original.String()))
		require.NoError(t, err)
		assert.Equal(t, original.String(), scanned.String())
	})

	t.Run("nil value", func(t *testing.T) {
		var scanned id.ID
		err := scanned.Scan(nil)
		require.NoError(t, err)
		assert.True(t, scanned.IsNil())
	})

	t.Run("empty string", func(t *testing.T) {
		var scanned id.ID
		err := scanned.Scan("")
		require.NoError(t, err)
		assert.True(t, scanned.IsNil())
	})

	t.Run("unsupported type", func(t *testing.T) {
		var scanned id.ID
		err := scanned.Scan(42)
		assert.Error(t, err)
	})
}

func TestIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		oid := id.NewObjectID()
		assert.False(t, seen[oid.String()], "duplicate ID generated")
		seen[oid.String()] = true
	}
}

func TestParseAny(t *testing.T) {
	original := id.NewObjectID()
	parsed, err := id.ParseAny(original.String())
	require.NoError(t, err)
	assert.Equal(t, original.String(), parsed.String())
}

func TestBSONRoundTrip(t *testing.T) {
	original := id.NewObjectID()

	// Marshal to BSON.
	bsonType, data, err := original.MarshalBSONValue()
	require.NoError(t, err)
	assert.Equal(t, byte(0x02), bsonType, "expected BSON string type")

	// Unmarshal back.
	var restored id.ID
	require.NoError(t, restored.UnmarshalBSONValue(bsonType, data))
	assert.Equal(t, original.String(), restored.String())

	// Nil round-trip.
	var nilID id.ID
	bsonType, data, err = nilID.MarshalBSONValue()
	require.NoError(t, err)
	assert.Equal(t, byte(0x0A), bsonType, "expected BSON null type")

	var restored2 id.ID
	require.NoError(t, restored2.UnmarshalBSONValue(bsonType, data))
	assert.True(t, restored2.IsNil())
}

func TestBSONUnmarshalInvalidType(t *testing.T) {
	var restored id.ID
	err := restored.UnmarshalBSONValue(0x01, []byte{0x00, 0x00, 0x00, 0x00})
	assert.Error(t, err)
}
