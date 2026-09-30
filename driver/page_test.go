package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPageKeys_FlatPagesByLastKey(t *testing.T) {
	keys := []string{"a.txt", "b.txt", "c.txt"}

	p1 := PageKeys(keys, ListConfig{MaxKeys: 2})
	assert.Equal(t, []string{"a.txt", "b.txt"}, p1.Keys)
	assert.Empty(t, p1.Prefixes)
	assert.Equal(t, "b.txt", p1.NextToken)

	p2 := PageKeys(keys, ListConfig{MaxKeys: 2, Cursor: p1.NextToken})
	assert.Equal(t, []string{"c.txt"}, p2.Keys)
	assert.Empty(t, p2.NextToken)
}

func TestPageKeys_ExactFitHasNoNextToken(t *testing.T) {
	p := PageKeys([]string{"a", "b"}, ListConfig{MaxKeys: 2})
	assert.Equal(t, []string{"a", "b"}, p.Keys)
	assert.Empty(t, p.NextToken)
}

func TestPageKeys_DelimiterFoldsKeysIntoPrefixes(t *testing.T) {
	keys := []string{"a/1", "a/2", "b/c/d", "top.txt"}

	p := PageKeys(keys, ListConfig{Delimiter: "/", MaxKeys: 100})
	assert.Equal(t, []string{"a/", "b/"}, p.Prefixes)
	assert.Equal(t, []string{"top.txt"}, p.Keys)
	assert.Empty(t, p.NextToken)
}

func TestPageKeys_PrefixAndDelimiterFoldBelowThePrefix(t *testing.T) {
	keys := []string{"logs/2026/a", "logs/2026/b", "logs/x", "other"}

	p := PageKeys(keys, ListConfig{Prefix: "logs/", Delimiter: "/", MaxKeys: 100})
	assert.Equal(t, []string{"logs/2026/"}, p.Prefixes)
	assert.Equal(t, []string{"logs/x"}, p.Keys)
}

func TestPageKeys_CursorAfterPrefixSkipsItsKeys(t *testing.T) {
	keys := []string{"a/1", "a/2", "b"}

	p1 := PageKeys(keys, ListConfig{Delimiter: "/", MaxKeys: 1})
	assert.Equal(t, []string{"a/"}, p1.Prefixes)
	assert.Empty(t, p1.Keys)
	assert.Equal(t, "a/", p1.NextToken)

	p2 := PageKeys(keys, ListConfig{Delimiter: "/", MaxKeys: 1, Cursor: p1.NextToken})
	assert.Empty(t, p2.Prefixes)
	assert.Equal(t, []string{"b"}, p2.Keys)
	assert.Empty(t, p2.NextToken)
}

func TestPageKeys_MultiCharacterDelimiter(t *testing.T) {
	p := PageKeys([]string{"a::1", "a::2", "b"}, ListConfig{Delimiter: "::", MaxKeys: 10})
	assert.Equal(t, []string{"a::"}, p.Prefixes)
	assert.Equal(t, []string{"b"}, p.Keys)
}

func TestPageKeys_ZeroMaxKeysUsesDefault(t *testing.T) {
	keys := make([]string, 1001)
	for i := range keys {
		keys[i] = string(rune('a'+i/676)) + string(rune('a'+(i/26)%26)) + string(rune('a'+i%26))
	}
	p := PageKeys(keys, ListConfig{MaxKeys: 0})
	assert.Len(t, p.Keys, 1000)
	assert.Equal(t, keys[999], p.NextToken)
}

func TestObjectIterator_CommonPrefixes(t *testing.T) {
	it := NewObjectIteratorWithPrefixes([]ObjectInfo{{Key: "top.txt"}}, []string{"a/"}, "tok")
	assert.Equal(t, []string{"a/"}, it.CommonPrefixes())
	assert.Equal(t, "tok", it.NextToken())

	plain := NewObjectIterator([]ObjectInfo{{Key: "x"}}, "")
	assert.Empty(t, plain.CommonPrefixes())
}
