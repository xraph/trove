package driver

import "strings"

// KeyPage is one page of a listing, computed from a full sorted key set.
type KeyPage struct {
	// Keys are the object keys on this page, in order.
	Keys []string
	// Prefixes are the common prefixes on this page, in order.
	Prefixes []string
	// NextToken is the last item on this page when more items follow,
	// and empty when the listing is complete.
	NextToken string
}

// PageKeys computes one page of a listing for drivers that hold, or can
// walk, their whole key set. The keys must be sorted lexicographically and
// free of duplicates.
//
// It applies cfg.Prefix, folds keys into common prefixes when cfg.Delimiter
// is set, skips every item at or before cfg.Cursor, and stops after
// cfg.MaxKeys items, counting prefixes and keys together. A MaxKeys of zero
// or less means 1000. The returned NextToken is the last item emitted, key
// or prefix, which is what the next call's Cursor must be.
func PageKeys(sorted []string, cfg ListConfig) KeyPage {
	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	var page KeyPage
	emitted := 0
	last := ""

	for _, key := range sorted {
		if !strings.HasPrefix(key, cfg.Prefix) {
			continue
		}

		item, isPrefix := key, false
		if cfg.Delimiter != "" {
			rest := key[len(cfg.Prefix):]
			if i := strings.Index(rest, cfg.Delimiter); i >= 0 {
				item = cfg.Prefix + rest[:i+len(cfg.Delimiter)]
				isPrefix = true
			}
		}

		if cfg.Cursor != "" && item <= cfg.Cursor {
			continue
		}
		// Keys sharing a prefix are contiguous in sorted order, so a
		// repeated prefix is always the one just emitted.
		if isPrefix && emitted > 0 && item == last {
			continue
		}

		if emitted == maxKeys {
			page.NextToken = last
			return page
		}

		if isPrefix {
			page.Prefixes = append(page.Prefixes, item)
		} else {
			page.Keys = append(page.Keys, key)
		}
		last = item
		emitted++
	}

	return page
}
