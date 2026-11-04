package buildinfo

import (
	"os"
	"runtime"
	"runtime/debug"
	"testing"
)

func vcsBuildInfo(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: settings}, true
	}
}

func noBuildInfo() (*debug.BuildInfo, bool) { return nil, false }

var vcs = vcsBuildInfo(
	debug.BuildSetting{Key: "vcs.revision", Value: "1111111111111111111111111111111111111111"},
	debug.BuildSetting{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
	debug.BuildSetting{Key: "vcs.modified", Value: "true"},
)

func TestInjectedValuesWin(t *testing.T) {
	got := resolve(injected{
		commit:    "2de7af1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		branch:    "develop",
		buildTime: "2026-10-26T10:00:00Z",
		dirty:     "false",
		version:   "v1.4.1-3-g2de7af1",
	}, vcs)
	want := Info{
		Version:   "v1.4.1-3-g2de7af1",
		Commit:    "2de7af1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Branch:    "develop",
		BuildTime: "2026-10-26T10:00:00Z",
		Dirty:     false,
		GoVersion: runtime.Version(),
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestFallsBackToReadBuildInfo(t *testing.T) {
	got := resolve(injected{}, vcs)
	if got.Commit != "1111111111111111111111111111111111111111" || got.BuildTime != "2026-01-02T03:04:05Z" || !got.Dirty {
		t.Fatalf("vcs fallback = %+v", got)
	}
	if got.Version != "111111111111-dirty" {
		t.Fatalf("version = %q, want short sha + -dirty", got.Version)
	}
	if got.Branch != "unknown" {
		t.Fatalf("branch = %q, want unknown (Go embeds no branch)", got.Branch)
	}
}

func TestPartialInjectionFillsTheRestFromBuildInfo(t *testing.T) {
	got := resolve(injected{commit: "abc123", branch: "main"}, vcs)
	if got.Commit != "abc123" || got.Branch != "main" || got.BuildTime != "2026-01-02T03:04:05Z" || !got.Dirty || got.Version != "abc123-dirty" {
		t.Fatalf("partial = %+v", got)
	}
}

func TestUnknownWithoutAnySource(t *testing.T) {
	for name, read := range map[string]func() (*debug.BuildInfo, bool){
		"no build info": noBuildInfo,
		"no vcs":        vcsBuildInfo(debug.BuildSetting{Key: "GOOS", Value: "linux"}),
	} {
		got := resolve(injected{}, read)
		want := Info{Version: "unknown", Commit: "unknown", Branch: "unknown", BuildTime: "unknown", GoVersion: runtime.Version()}
		if got != want {
			t.Fatalf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

func TestGetUsesPackageVars(t *testing.T) {
	saved := []string{Commit, Branch, BuildTime, Dirty, Version}
	t.Cleanup(func() {
		Commit, Branch, BuildTime, Dirty, Version = saved[0], saved[1], saved[2], saved[3], saved[4]
	})
	Commit, Branch, BuildTime, Dirty, Version = "feedface", "develop", "2026-10-26T00:00:00Z", "true", ""
	got := Get()
	if got.Commit != "feedface" || got.Branch != "develop" || got.BuildTime != "2026-10-26T00:00:00Z" || !got.Dirty || got.Version != "feedface-dirty" {
		t.Fatalf("Get() = %+v", got)
	}
}

// TestLinkerInjection checks the -X symbol paths the Dockerfiles use. It
// runs only when built with them, e.g.
//
//	BUILDINFO_EXPECT_COMMIT=abc go test -ldflags "-X webrtc-sip-gateway/internal/buildinfo.Commit=abc ..." ./internal/buildinfo
func TestLinkerInjection(t *testing.T) {
	want := os.Getenv("BUILDINFO_EXPECT_COMMIT")
	if want == "" {
		t.Skip("BUILDINFO_EXPECT_COMMIT not set")
	}
	got := Get()
	if got.Commit != want || got.Branch != os.Getenv("BUILDINFO_EXPECT_BRANCH") ||
		got.BuildTime != os.Getenv("BUILDINFO_EXPECT_BUILD_TIME") || got.Version != os.Getenv("BUILDINFO_EXPECT_VERSION") ||
		got.Dirty != (os.Getenv("BUILDINFO_EXPECT_DIRTY") == "true") {
		t.Fatalf("linker-injected Get() = %+v", got)
	}
}
