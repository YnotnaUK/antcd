package git

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Watcher struct {
	RepoURL    string
	Branch     string
	Token      string
	LastCommit string
}

func NewWatcher(repoURL, branch, token string) *Watcher {
	return &Watcher{
		RepoURL: repoURL,
		Branch:  branch,
		Token:   token,
	}
}

// CheckForUpdates returns true if the remote commit hash differs from the last seen hash.
func (w *Watcher) CheckForUpdates() (bool, string, error) {
	rem := gogit.NewRemote(memory.NewStorage(), &gitcfg.RemoteConfig{
		Name: "origin",
		URLs: []string{w.RepoURL},
	})

	var auth *http.BasicAuth
	if w.Token != "" {
		auth = &http.BasicAuth{
			Username: "oauth2",
			Password: w.Token,
		}
	}

	// Light check: fetch remote references only, no cloning required
	refs, err := rem.List(&gogit.ListOptions{
		Auth: auth,
	})
	if err != nil {
		return false, "", fmt.Errorf("listing remote references: %w", err)
	}

	targetRef := "refs/heads/" + w.Branch
	var currentCommit string

	for _, ref := range refs {
		if ref.Name().String() == targetRef {
			currentCommit = ref.Hash().String()
			break
		}
	}

	if currentCommit == "" {
		return false, "", fmt.Errorf("branch '%s' not found on remote", w.Branch)
	}

	if w.LastCommit == "" {
		w.LastCommit = currentCommit
		return true, currentCommit, nil // First run detected
	}

	if currentCommit != w.LastCommit {
		w.LastCommit = currentCommit
		return true, currentCommit, nil // New commit detected
	}

	return false, currentCommit, nil
}
