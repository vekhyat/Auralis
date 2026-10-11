package backend

// testCommunityCandidateCatalog is TEST-ONLY. It reproduces the historical
// public candidate list (18 hosted presets + 4 empty server templates) so the
// live audit stays reproducible without shipping any of those profiles in the
// production defaults. Production defaultCommunitySources stays empty; this
// helper is compiled into tests only and never ships in the app.
func testCommunityCandidateCatalog() []CommunitySource {
	var rows []CommunitySource
	for _, host := range []string{"monochrome-api.samidy.com", "eu-central.monochrome.tf", "us-west.monochrome.tf", "arran.monochrome.tf", "api.monochrome.tf", "triton.squid.wtf", "wolf.qqdl.site", "maus.qqdl.site", "vogel.qqdl.site", "katze.qqdl.site", "hund.qqdl.site", "tidal.kinoplus.online", "hifi.p1nkhamster.xyz"} {
		rows = append(rows, CommunitySource{ID: "hifi-" + host, Name: "Hi-Fi / " + host, Service: "tidal", Protocol: "hifi", BaseURL: "https://" + host})
	}
	return append(rows,
		CommunitySource{ID: "qobuz-squid", Name: "SquidWTF Qobuz", Service: "qobuz", Protocol: "qobuz-dl", BaseURL: "https://qobuz.squid.wtf"},
		CommunitySource{ID: "dab-xyz", Name: "DAB Music", Service: "qobuz", Protocol: "dab", BaseURL: "https://dabmusic.xyz", CredentialEnv: "AURALIS_DAB_COOKIE", CredentialType: "cookie"},
		CommunitySource{ID: "dab-yeet", Name: "DAB Yeet", Service: "qobuz", Protocol: "dab", BaseURL: "https://dab.yeet.su", CredentialEnv: "AURALIS_DAB_COOKIE", CredentialType: "cookie"},
		CommunitySource{ID: "lucida-qobuz", Name: "Lucida / Qobuz", Service: "qobuz", Protocol: "lucida", BaseURL: "https://lucida.to", CredentialEnv: "AURALIS_LUCIDA_COOKIE", CredentialType: "cookie"},
		CommunitySource{ID: "lucida-amazon", Name: "Lucida / Amazon Music", Service: "amazon", Protocol: "lucida", BaseURL: "https://lucida.to", CredentialEnv: "AURALIS_LUCIDA_COOKIE", CredentialType: "cookie"},
		CommunitySource{ID: "qobuz-rest", Name: "Qobuz REST server", Service: "qobuz", Protocol: "qobuz-rest", CredentialEnv: "AURALIS_QOBUZ_REST_KEY", CredentialType: "api_key"},
		CommunitySource{ID: "qobuz-dl", Name: "Qobuz-DL server", Service: "qobuz", Protocol: "qobuz-dl"},
		CommunitySource{ID: "bryan-qobuz", Name: "Bryan-DL / Qobuz", Service: "qobuz", Protocol: "qobuz-dl"},
		CommunitySource{ID: "octo-fiesta", Name: "Octo-Fiesta / Subsonic", Service: "qobuz", Protocol: "subsonic", CredentialEnv: "AURALIS_SUBSONIC_AUTH", CredentialType: "subsonic"},
	)
}
