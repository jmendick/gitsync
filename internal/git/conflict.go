package git

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
)

// GetConflictVersions returns different versions of a file in conflict
func (m *GitRepositoryManager) GetConflictVersions(repo *git.Repository, path string) ([]ConflictVersion, error) {
	w, err := repo.Worktree()
	if err != nil {
		return nil, err
	}

	status, err := w.Status()
	if err != nil {
		return nil, err
	}

	fileStatus := status.File(path)
	// Check if file is actually in conflict using git status
	if fileStatus.Staging != 'U' { // 'U' represents unmerged status
		return nil, fmt.Errorf("file is not in conflict")
	}

	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}

	var versions []ConflictVersion
	for _, e := range idx.Entries {
		if e.Name == path && (e.Stage == 2 || e.Stage == 3) {
			obj, err := repo.BlobObject(e.Hash)
			if err != nil {
				continue
			}

			content, err := obj.Reader()
			if err != nil {
				continue
			}

			data, err := io.ReadAll(content)
			content.Close()
			if err != nil {
				continue
			}

			version := ConflictVersion{
				PeerID:    fmt.Sprintf("stage-%d", e.Stage),
				Hash:      e.Hash.String(),
				Content:   data,
				Timestamp: time.Now(),
			}
			versions = append(versions, version)
		}
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no conflict versions found for file")
	}

	return versions, nil
}

// ResolveConflict resolves a conflict by choosing a strategy
func (m *GitRepositoryManager) ResolveConflict(repo *git.Repository, path string, strategy string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	versions, err := m.GetConflictVersions(repo, path)
	if err != nil {
		return err
	}

	if len(versions) < 2 {
		return fmt.Errorf("not enough versions to resolve conflict")
	}

	var content []byte
	switch strategy {
	case "ours":
		// Use our version (stage 2)
		for _, v := range versions {
			if v.PeerID == "stage-2" {
				content = v.Content
				break
			}
		}
	case "theirs":
		// Use their version (stage 3)
		for _, v := range versions {
			if v.PeerID == "stage-3" {
				content = v.Content
				break
			}
		}
	case "union":
		// Combine both versions
		content = []byte("<<<<<<< ours\n")
		for _, v := range versions {
			if v.PeerID == "stage-2" {
				content = append(content, v.Content...)
				content = append(content, []byte("\n=======\n")...)
			}
		}
		for _, v := range versions {
			if v.PeerID == "stage-3" {
				content = append(content, v.Content...)
			}
		}
		content = append(content, []byte("\n>>>>>>> END\n")...)
	default:
		return fmt.Errorf("unknown conflict resolution strategy: %s", strategy)
	}

	if len(content) == 0 {
		return fmt.Errorf("failed to get content for strategy: %s", strategy)
	}

	// Write the resolved content
	file, err := w.Filesystem.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(content); err != nil {
		return err
	}

	// Stage the resolved file
	_, err = w.Add(path)
	return err
}
