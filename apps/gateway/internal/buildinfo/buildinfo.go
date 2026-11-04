// Package buildinfo reports which gateway build is running.
//
// The Docker builds inject the values with
//
//	-ldflags "-X webrtc-sip-gateway/internal/buildinfo.Commit=<sha> ..."
//
// because .git is not in the image build context. A plain `go build` in a
// checkout falls back to the vcs.* settings Go embeds; anything still
// missing is "unknown".
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Set at build time with -ldflags -X. Empty means not injected.
var (
	Commit    = "" // full git sha
	Branch    = "" // git rev-parse --abbrev-ref HEAD
	BuildTime = "" // RFC3339 UTC
	Dirty     = "" // "true" or "false"
	Version   = "" // git describe --tags --always --dirty
)

const unknown = "unknown"

// Info is the JSON body of GET /api/version.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Branch    string `json:"branch"`
	BuildTime string `json:"buildTime"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"goVersion"`
}

// Get returns the running build. Injected values win; each missing field
// falls back to debug.ReadBuildInfo, then to "unknown".
func Get() Info {
	return resolve(injected{
		commit:    Commit,
		branch:    Branch,
		buildTime: BuildTime,
		dirty:     Dirty,
		version:   Version,
	}, debug.ReadBuildInfo)
}

type injected struct {
	commit, branch, buildTime, dirty, version string
}

func resolve(in injected, readBuildInfo func() (*debug.BuildInfo, bool)) Info {
	commit := strings.TrimSpace(in.commit)
	buildTime := strings.TrimSpace(in.buildTime)
	dirtyText := strings.TrimSpace(in.dirty)
	if commit == "" || buildTime == "" || dirtyText == "" {
		if bi, ok := readBuildInfo(); ok && bi != nil {
			for _, setting := range bi.Settings {
				switch setting.Key {
				case "vcs.revision":
					if commit == "" {
						commit = setting.Value
					}
				case "vcs.time":
					if buildTime == "" {
						buildTime = setting.Value
					}
				case "vcs.modified":
					if dirtyText == "" {
						dirtyText = setting.Value
					}
				}
			}
		}
	}

	info := Info{
		Version:   strings.TrimSpace(in.version),
		Commit:    orUnknown(commit),
		Branch:    orUnknown(strings.TrimSpace(in.branch)),
		BuildTime: orUnknown(buildTime),
		Dirty:     parseBool(dirtyText),
		GoVersion: runtime.Version(),
	}
	if info.Version == "" {
		info.Version = unknown
		if commit != "" {
			info.Version = shortSHA(commit)
			if info.Dirty {
				info.Version += "-dirty"
			}
		}
	}
	return info
}

func orUnknown(v string) string {
	if v == "" {
		return unknown
	}
	return v
}

func parseBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
