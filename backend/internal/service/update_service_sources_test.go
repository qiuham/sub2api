//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// 返回互不相同的仓库数据，避免单一 stub 掩盖更新源串用。
type releaseSourceClient struct {
	updateServiceGitHubClientStub
	latestCalls []string
	recentCalls []string
	upstreamErr error
}

func (s *releaseSourceClient) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.latestCalls = append(s.latestCalls, repo)
	switch repo {
	case githubRepo:
		return &GitHubRelease{TagName: "v0.2.8-N.3", HTMLURL: "https://github.com/qiuham/sub2api/releases/tag/v0.2.8-N.3"}, nil
	case upstreamRepo:
		if s.upstreamErr != nil {
			return nil, s.upstreamErr
		}
		return &GitHubRelease{TagName: "v0.2.9", HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.9"}, nil
	default:
		return nil, errors.New("unexpected repository")
	}
}

func (s *releaseSourceClient) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	s.recentCalls = append(s.recentCalls, repo)
	return []*GitHubRelease{{TagName: "v0.2.8-N.2"}}, nil
}

func TestUpdateSourcesRemainSeparate(t *testing.T) {
	client := &releaseSourceClient{}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.8-N.2", "release")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, []string{githubRepo, upstreamRepo}, client.latestCalls)
	require.Equal(t, "0.2.8-N.3", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.Equal(t, "0.2.9", info.UpstreamLatestVersion)
	require.True(t, info.HasUpstreamUpdate)
	require.Contains(t, info.ReleaseInfo.HTMLURL, githubRepo)
	require.Contains(t, info.UpstreamReleaseInfo.HTMLURL, upstreamRepo)
	cached, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, info.UpstreamLatestVersion, cached.UpstreamLatestVersion)
	require.Len(t, client.latestCalls, 2)
}

func TestUpstreamCheckFailureKeepsOwnUpdate(t *testing.T) {
	client := &releaseSourceClient{upstreamErr: errors.New("fixture upstream unavailable")}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.8-N.2", "release")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, "0.2.8-N.3", info.LatestVersion)
	require.Empty(t, info.UpstreamLatestVersion)
	require.Contains(t, info.Warning, "upstream check unavailable")
}

func TestRollbackSourceIsOwnRepository(t *testing.T) {
	client := &releaseSourceClient{}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.8-N.3", "release")
	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "0.2.8-N.2", versions[0].Version)
	require.Equal(t, []string{githubRepo}, client.recentCalls)
}

func TestUpstreamOnlyUpdateNeverStartsInstall(t *testing.T) {
	client := &releaseSourceClient{}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.8-N.3", "release")
	// 嵌入 stub 的下载方法会 panic，因此误触安装也会使测试失败。
	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrNoUpdateAvailable)
	require.Equal(t, []string{githubRepo, upstreamRepo}, client.latestCalls)
}
