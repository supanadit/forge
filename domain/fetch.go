package domain

// FetchType enumerates the supported source acquisition methods.
type FetchType string

const (
	// FetchTypeArchive downloads and extracts a compressed archive (tar.gz, zip).
	FetchTypeArchive FetchType = "archive"
	// FetchTypeGit clones a git repository.
	FetchTypeGit FetchType = "git"
)

// FetchSpec describes where source code or a binary comes from.
type FetchSpec struct {
	// Type is the fetch method (archive or git).
	Type FetchType
	// Archive is populated when Type == FetchTypeArchive.
	Archive *ArchiveFetch
	// Git is populated when Type == FetchTypeGit.
	Git *GitFetch
}

// ArchiveFetch downloads a compressed archive and extracts it.
type ArchiveFetch struct {
	// URL is the archive download URL.
	URL string
	// ChecksumType is the checksum algorithm (e.g. "sha256"); empty to skip.
	ChecksumType string
	// Checksum is the expected checksum value; empty to skip.
	Checksum string
	// Dest overrides the extraction destination dir; empty uses the cache.
	Dest string
}

// GitFetch clones a git repository.
type GitFetch struct {
	// URL is the git remote URL.
	URL string
	// Ref is the branch, tag, or commit to check out (empty uses default branch).
	Ref string
	// Depth is the clone depth (1 = shallow); 0 uses full clone.
	Depth int
	// Dest overrides the clone destination dir; empty uses the cache.
	Dest string
}
