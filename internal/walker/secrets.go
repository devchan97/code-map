package walker

import (
	"path/filepath"
	"regexp"
	"strings"
)

// secretFileExactNames is the set of filenames (basename only) that are
// always treated as secret files regardless of path.
var secretFileExactNames = map[string]struct{}{
	".env": {},
}

// secretFilePrefixes lists basenames-prefixes that identify secret files.
var secretFilePrefixes = []string{
	".env.",  // .env.local, .env.production, …
	"id_rsa", // id_rsa, id_rsa.pub, …
	"id_ed25519",
	"id_ecdsa",
	"id_dsa",
}

// secretFileExts lists file extensions (lowercase, with dot) that are always
// treated as secret key material.
var secretFileExts = map[string]struct{}{
	".pem": {},
	".key": {},
	".p12": {},
	".pfx": {},
}

// Precompiled secret-content patterns.
var (
	reAWSKey  = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	rePEMHead = regexp.MustCompile(`-----BEGIN `)
)

// IsSecretFile reports whether the file at relPath (repo-relative,
// forward-slash normalized) looks like a secrets file and should be
// excluded from indexing before its content is read.
func IsSecretFile(relPath string) bool {
	base := filepath.Base(relPath)
	baseLower := strings.ToLower(base)

	// Exact match.
	if _, ok := secretFileExactNames[baseLower]; ok {
		return true
	}

	// Extension match.
	ext := strings.ToLower(filepath.Ext(baseLower))
	if _, ok := secretFileExts[ext]; ok {
		return true
	}

	// Prefix match.
	for _, pfx := range secretFilePrefixes {
		if strings.HasPrefix(baseLower, pfx) {
			return true
		}
	}

	return false
}

// ContainsSecretPattern reports whether content contains a known secret
// pattern such as an AWS access-key prefix or a PEM header.
// Precompiled regexes are used for efficiency.
func ContainsSecretPattern(content []byte) bool {
	return reAWSKey.Match(content) || rePEMHead.Match(content)
}
