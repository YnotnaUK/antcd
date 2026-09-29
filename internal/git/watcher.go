package git

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

// CheckForUpdates returns (true, commitHash) if remote differs from the last applied commit.
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

	refs, err := rem.List(&gogit.ListOptions{Auth: auth})
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

	// Needs apply if remote differs from our last successfully applied commit
	if currentCommit != w.LastCommit {
		return true, currentCommit, nil
	}

	return false, currentCommit, nil
}

// RecordSuccess marks the commit as successfully applied.
func (w *Watcher) RecordSuccess(commit string) {
	w.LastCommit = commit
}

// CloneToDir clones the target branch into a temporary local directory.
func (w *Watcher) CloneToDir(targetDir string) error {
	var auth *http.BasicAuth
	if w.Token != "" {
		auth = &http.BasicAuth{
			Username: "oauth2",
			Password: w.Token,
		}
	}

	_, err := gogit.PlainClone(targetDir, false, &gogit.CloneOptions{
		URL:           w.RepoURL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + w.Branch),
		SingleBranch:  true,
		Depth:         1,
		Auth:          auth,
	})
	if err != nil {
		return fmt.Errorf("cloning repo: %w", err)
	}

	return nil
}
