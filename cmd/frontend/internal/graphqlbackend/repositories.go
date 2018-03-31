package graphqlbackend

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	graphql "github.com/neelance/graphql-go"
	"sourcegraph.com/sourcegraph/sourcegraph/cmd/frontend/internal/app/envvar"
	"sourcegraph.com/sourcegraph/sourcegraph/cmd/frontend/internal/backend"
	"sourcegraph.com/sourcegraph/sourcegraph/cmd/frontend/internal/db"
	"sourcegraph.com/sourcegraph/sourcegraph/cmd/frontend/internal/pkg/types"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/api"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/gitserver"
)

func (r *siteResolver) Repositories(args *struct {
	connectionArgs
	Query           *string
	Enabled         bool
	Disabled        bool
	Cloned          bool
	CloneInProgress bool
	NotCloned       bool
	Indexed         bool
	NotIndexed      bool
}) (*repositoryConnectionResolver, error) {
	opt := db.ReposListOptions{
		Enabled:  args.Enabled,
		Disabled: args.Disabled,
	}
	if args.Query != nil {
		opt.Query = *args.Query
	}
	args.connectionArgs.set(&opt.LimitOffset)
	return &repositoryConnectionResolver{
		opt:             opt,
		cloned:          args.Cloned,
		cloneInProgress: args.CloneInProgress,
		notCloned:       args.NotCloned,
		indexed:         args.Indexed,
		notIndexed:      args.NotIndexed,
	}, nil
}

type repositoryConnectionResolver struct {
	opt             db.ReposListOptions
	cloned          bool
	cloneInProgress bool
	notCloned       bool
	indexed         bool
	notIndexed      bool

	// cache results because they are used by multiple fields
	once  sync.Once
	repos []*types.Repo
	err   error
}

func (r *repositoryConnectionResolver) compute(ctx context.Context) ([]*types.Repo, error) {
	r.once.Do(func() {
		opt2 := r.opt

		if envvar.SourcegraphDotComMode() {
			// Don't allow non-admins to perform huge queries on Sourcegraph.com.
			if isSiteAdmin := backend.CheckCurrentUserIsSiteAdmin(ctx) == nil; !isSiteAdmin {
				if opt2.LimitOffset == nil {
					opt2.LimitOffset = &db.LimitOffset{Limit: 1000}
				}
			}
		}

		if opt2.LimitOffset != nil {
			tmp := *opt2.LimitOffset
			opt2.LimitOffset = &tmp
			opt2.Limit++ // so we can detect if there is a next page
		}

		var indexed map[api.RepoURI]bool
		isIndexed := func(repo api.RepoURI) bool {
			if zoektCache == nil {
				return true // do not need index
			}
			return indexed[api.RepoURI(strings.ToLower(string(repo)))]
		}
		if zoektCache != nil && (!r.indexed || !r.notIndexed) {
			listCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			indexedRepos, err := zoektCache.ListAll(listCtx)
			if err != nil {
				r.err = err
				return
			}
			indexed = make(map[api.RepoURI]bool, len(indexedRepos.Repos))
			for _, repo := range indexedRepos.Repos {
				indexed[api.RepoURI(strings.ToLower(string(repo.Repository.Name)))] = true
			}
		}

		for {
			repos, err := backend.Repos.List(ctx, opt2)
			if err != nil {
				r.err = err
				return
			}
			reposFromDB := len(repos)

			if !r.cloned || !r.cloneInProgress || !r.notCloned {
				// Query gitserver to filter by repository clone status.
				keepRepos := repos[:0]
				for _, repo := range repos {
					info, err := gitserver.DefaultClient.RepoInfo(ctx, repo.URI)
					if err != nil {
						r.err = err
						return
					}
					if (r.cloned && info.Cloned && !info.CloneInProgress) || (r.cloneInProgress && info.CloneInProgress) || (r.notCloned && !info.Cloned && !info.CloneInProgress) {
						keepRepos = append(keepRepos, repo)
					}
				}
				repos = keepRepos
			}

			if !r.indexed || !r.notIndexed {
				keepRepos := repos[:0]
				for _, repo := range repos {
					indexed := isIndexed(repo.URI)
					if (r.indexed && indexed) || (r.notIndexed && !indexed) {
						keepRepos = append(keepRepos, repo)
					}
				}
				repos = keepRepos
			}

			r.repos = append(r.repos, repos...)
			if opt2.LimitOffset == nil {
				break
			} else {
				if len(r.repos) >= opt2.Limit {
					break
				}
				if reposFromDB < opt2.Limit {
					break
				}
				opt2.Offset += opt2.Limit
			}
		}
	})
	return r.repos, r.err
}

func (r *repositoryConnectionResolver) Nodes(ctx context.Context) ([]*repositoryResolver, error) {
	repos, err := r.compute(ctx)
	if err != nil {
		return nil, err
	}
	resolvers := make([]*repositoryResolver, 0, len(repos))
	for i, repo := range repos {
		if r.opt.LimitOffset != nil && i == r.opt.Limit {
			break
		}
		resolvers = append(resolvers, &repositoryResolver{repo: repo})
	}
	return resolvers, nil
}

func (r *repositoryConnectionResolver) TotalCount(ctx context.Context, args *struct {
	Precise bool
}) (countptr *int32, err error) {
	i32ptr := func(v int32) *int32 {
		return &v
	}

	if !r.cloned || !r.cloneInProgress || !r.notCloned {
		// Don't support counting if filtering by clone status.
		return nil, nil
	}
	if !r.indexed || !r.notIndexed {
		// Don't support counting if filtering by index status.
		return nil, nil
	}

	if args.Precise {
		// Only site admins can perform precise counts, because it is a slow operation.
		if err := backend.CheckCurrentUserIsSiteAdmin(ctx); err != nil {
			return nil, err
		}
	}

	// Counting repositories is slow on Sourcegraph.com. Don't wait very long for an exact count.
	if !args.Precise && envvar.SourcegraphDotComMode() {
		if len(r.opt.Query) < 4 {
			return nil, nil
		}

		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		defer func() {
			if ctx.Err() == context.DeadlineExceeded {
				countptr = nil
				err = nil
			}
		}()
	}

	count, err := db.Repos.Count(ctx, r.opt)
	return i32ptr(int32(count)), err
}

func (r *repositoryConnectionResolver) PageInfo(ctx context.Context) (*pageInfo, error) {
	repos, err := r.compute(ctx)
	if err != nil {
		return nil, err
	}
	return &pageInfo{hasNextPage: r.opt.LimitOffset != nil && len(repos) > r.opt.Limit}, nil
}

func (r *schemaResolver) AddRepository(ctx context.Context, args *struct {
	Name string
}) (*repositoryResolver, error) {
	// 🚨 SECURITY: Only site admins can add repositories.
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx); err != nil {
		return nil, err
	}

	uri := api.RepoURI(args.Name)
	if err := backend.Repos.Add(ctx, uri); err != nil {
		return nil, err
	}
	repo, err := backend.Repos.GetByURI(ctx, uri)
	if err != nil {
		return nil, err
	}
	return &repositoryResolver{repo: repo}, nil
}

func (r *schemaResolver) SetRepositoryEnabled(ctx context.Context, args *struct {
	Repository graphql.ID
	Enabled    bool
}) (*EmptyResponse, error) {
	// 🚨 SECURITY: Only site admins can enable/disable repositories, because it's a site-wide
	// and semi-destructive action.
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx); err != nil {
		return nil, err
	}

	repo, err := repositoryByID(ctx, args.Repository)
	if err != nil {
		return nil, err
	}
	if err := db.Repos.SetEnabled(ctx, repo.repo.ID, args.Enabled); err != nil {
		return nil, err
	}

	// Trigger update when enabling.
	if args.Enabled {
		gitserverRepo, err := backend.Repos.GitserverRepoInfo(ctx, repo.repo)
		if err != nil {
			return nil, err
		}
		if err := gitserver.DefaultClient.EnqueueRepoUpdate(ctx, gitserverRepo); err != nil {
			return nil, err
		}
		if err := backend.Repos.RefreshIndex(ctx, repo.repo); err != nil {
			return nil, err
		}
	}

	return &EmptyResponse{}, nil
}

func (r *schemaResolver) SetAllRepositoriesEnabled(ctx context.Context, args *struct {
	Enabled bool
}) (*EmptyResponse, error) {
	// Only usable for self-hosted instances
	if envvar.SourcegraphDotComMode() {
		return nil, errors.New("Not available on sourcegraph.com")
	}
	// 🚨 SECURITY: Only site admins can enable/disable repositories, because it's a site-wide
	// and semi-destructive action.
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx); err != nil {
		return nil, err
	}

	var listArgs db.ReposListOptions
	if args.Enabled {
		listArgs = db.ReposListOptions{Disabled: true}
	} else {
		listArgs = db.ReposListOptions{Enabled: true}
	}
	reposList, err := db.Repos.List(ctx, listArgs)
	if err != nil {
		return nil, err
	}

	for _, repo := range reposList {
		if err := db.Repos.SetEnabled(ctx, repo.ID, args.Enabled); err != nil {
			return nil, err
		}
	}
	return &EmptyResponse{}, nil
}

func (r *schemaResolver) DeleteRepository(ctx context.Context, args *struct {
	Repository graphql.ID
}) (*EmptyResponse, error) {
	// 🚨 SECURITY: Only site admins can delete repositories, because it's a site-wide
	// and semi-destructive action.
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx); err != nil {
		return nil, err
	}

	id, err := unmarshalRepositoryID(args.Repository)
	if err != nil {
		return nil, err
	}
	if err := db.Repos.Delete(ctx, id); err != nil {
		return nil, err
	}
	return &EmptyResponse{}, nil
}

func repoIDsToInt32s(repoIDs []api.RepoID) []int32 {
	int32s := make([]int32, len(repoIDs))
	for i, repoID := range repoIDs {
		int32s[i] = int32(repoID)
	}
	return int32s
}

func repoURIsToStrings(repoURIs []api.RepoURI) []string {
	strings := make([]string, len(repoURIs))
	for i, repoURI := range repoURIs {
		strings[i] = string(repoURI)
	}
	return strings
}
