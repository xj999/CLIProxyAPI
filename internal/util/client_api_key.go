package util

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

const usageSourcePrefixKey = "k:"

// NormalizeClientAPIKeyID returns the stable usage identifier for a top-level client API key.
func NormalizeClientAPIKeyID(apiKey string) string {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return ""
	}

	const (
		fnvOffsetBasis uint64 = 0xcbf29ce484222325
		fnvPrime       uint64 = 0x100000001b3
	)

	hash := fnvOffsetBasis
	for _, codeUnit := range utf16.Encode([]rune(trimmed)) {
		hash ^= uint64(codeUnit)
		hash *= fnvPrime
	}

	return usageSourcePrefixKey + fmt.Sprintf("%016x", hash)
}

// MaskClientAPIKey obscures a top-level client API key using the same compact format as the management UI.
func MaskClientAPIKey(apiKey string) string {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return ""
	}

	const maskedLength = 10
	visibleChars := 2
	if len(trimmed) < 4 {
		visibleChars = 1
	}
	start := trimmed[:visibleChars]
	end := trimmed[len(trimmed)-visibleChars:]
	maskLen := maskedLength - visibleChars*2
	if maskLen < 1 {
		maskLen = 1
	}
	return start + strings.Repeat("*", maskLen) + end
}
