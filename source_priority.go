package main

import "github.com/vekhyat/Auralis/backend"

func (a *App) GetDownloadSources() []backend.DownloadSource   { return backend.DownloadSources() }
func (a *App) GetSourceBenchmarks() []backend.SourceBenchmark { return backend.SourceBenchmarks() }
func (a *App) RankDownloadServices(services []string, quality string) []string {
	return backend.RankedDownloadServices(services, quality)
}

func (a *App) GetResourceSources() []backend.DownloadSource { return backend.ResourceSources() }

func (a *App) ListCommunitySources() ([]backend.CommunitySource, error) {
	return backend.ListCommunitySources()
}
func (a *App) SaveCommunitySources(sources []backend.CommunitySource) error {
	return backend.SaveCommunitySources(sources)
}
func (a *App) GetCommunitySourceChecks() []backend.CommunitySourceCheck {
	return backend.GetCommunitySourceChecks()
}
func (a *App) CheckCommunitySource(id string) (backend.CommunitySourceCheck, error) {
	return backend.CheckCommunitySource(id, false)
}
func (a *App) TestCommunitySourceDownload(id string) (backend.CommunitySourceCheck, error) {
	return backend.CheckCommunitySource(id, true)
}
