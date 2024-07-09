package timetracedb

import "fmt"

// These constants follow the semantic versioning 2.0.0 spec (http://semver.org/)
type Version struct {
	Meta  string
	Major uint8
	Minor uint8
	Patch uint8
}

var version = Version{
	Major: 0,
	Minor: 1,
	Patch: 0,
	Meta:  "beta",
}

func StringVersion() string {
	v := fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)

	if version.Meta != "" {
		v = fmt.Sprintf("%s-%s", v, version.Meta)
	}

	return v
}
