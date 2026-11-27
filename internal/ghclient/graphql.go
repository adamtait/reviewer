// SPDX-License-Identifier: MIT

package ghclient

import (
	"context"
	"fmt"
)

// BlameRange is a run of lines, 1-based and inclusive, that blame attributes to
// one commit.
type BlameRange struct {
	StartLine int
	EndLine   int
	// Commit is the full SHA of the commit that last wrote these lines.
	Commit string
}

// Blame reads which commit last wrote each line of a file as it stood at a
// commit. It is a method on REST and not part of Client: only outcome
// measurement asks, to tell whether a later pull request changed lines an
// earlier one wrote, and REST has no blame at all.
//
// The response shape is GitHub's documented one for Commit.blame, and has not
// been checked against a live response; the test records that shape, not one
// GitHub sent.
func (c *REST) Blame(ctx context.Context, repo Repo, sha, path string) ([]BlameRange, error) {
	const query = `
query($owner:String!, $name:String!, $oid:GitObjectID!, $path:String!) {
  repository(owner:$owner, name:$name) {
    object(oid:$oid) {
      ... on Commit {
        blame(path:$path) {
          ranges { startingLine endingLine commit { oid } }
        }
      }
    }
  }
}`
	var resp struct {
		Data struct {
			Repository struct {
				// Object is a pointer so that a commit GitHub does not have
				// reads as absent rather than as a file with no lines.
				Object *struct {
					Blame struct {
						Ranges []struct {
							StartingLine int `json:"startingLine"`
							EndingLine   int `json:"endingLine"`
							Commit       struct {
								OID string `json:"oid"`
							} `json:"commit"`
						} `json:"ranges"`
					} `json:"blame"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	vars := map[string]any{"owner": repo.Owner, "name": repo.Name, "oid": sha, "path": path}
	if err := c.graphQL(ctx, query, vars, &resp); err != nil {
		return nil, err
	}
	// GraphQL reports errors with a 200, so the body has to be checked. A path
	// that does not exist at the commit is one of them.
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("blaming %s at %s: %s", path, sha, resp.Errors[0].Message)
	}
	if resp.Data.Repository.Object == nil {
		return nil, fmt.Errorf("blaming %s at %s: %w", path, sha, ErrNotFound)
	}
	ranges := resp.Data.Repository.Object.Blame.Ranges
	out := make([]BlameRange, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, BlameRange{StartLine: r.StartingLine, EndLine: r.EndingLine, Commit: r.Commit.OID})
	}
	return out, nil
}
