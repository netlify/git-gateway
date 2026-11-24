package api

import (
	"encoding/hex"
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const maxURLDecodeDepth = 3

var (
	errProxyPathNotAllowed  = errors.New("path is not on the allow-list")
	errProxyPathEscapesRepo = errors.New("path escapes the repository")
	errProxyIllegalPath     = errors.New("semicolons and backslashes are not allowed")
	errProxyEncodingDepth   = errors.New("too many levels of percent-encoding")
)

// validateProxyPath checks whether a request may be forwarded to the git provider. It primarily
// checks for path traversal attacks. This is meant only to validate proxied requests: Callers
// should decide whether to clean/sanitize forwarded URLs. (In general, prefer validating and then
// let the forge resolve the user's URL as is so we're not silently and opaquely transforming
// requests.)
//
// repoPath is the request path relative to the repository's API root (no leading slash), rawQuery
// is the query string as received, and allowed is matched against the whole cleaned repoPath (so it
// must be anchored at both ends).
//
// Returns errProxyPathNotAllowed for a well-formed path that is not on the allow-list and a
// different error for a request that is malformed.
func validateProxyPath(allowed *regexp.Regexp, repoPath, rawQuery string) error {
	for depth := 0; ; depth++ {
		// GitHub resolves "..;" as "..", and "..\" is a known variant of the same trick
		if strings.ContainsAny(repoPath, `;\`) || strings.Contains(rawQuery, ";") {
			return errProxyIllegalPath
		}
		cleaned := path.Clean(repoPath)
		if !filepath.IsLocal(cleaned) {
			return errProxyPathEscapesRepo
		}
		if !allowed.MatchString(cleaned) {
			return errProxyPathNotAllowed
		}

		nextPath, nextQuery := percentDecode(repoPath), percentDecode(rawQuery)
		if nextPath == repoPath && nextQuery == rawQuery {
			return nil
		}
		// Limit decoding depth to prevent traversal attacks
		if depth == maxURLDecodeDepth {
			return errProxyEncodingDepth
		}
		repoPath, rawQuery = nextPath, nextQuery
	}
}

// percentDecode decodes well-formed escaped characters and leaves malformed ones in place. A stdlib
// function like url.PathUnescape fails on malformed escapes like these, but Git and GitHub permit
// paths with literal percents in their names (e.g. `50%off.png`), so we want to permit those paths.
//
// Our objective here is to find illegal escapes, not validate the URL (that's the forge's job).
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if decoded, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
				b.WriteByte(decoded[0])
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
