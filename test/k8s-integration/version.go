/*
Copyright 2025 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

var (
	versionNum           = `(0|[1-9][0-9]*)`
	internalPatchVersion = `(\-[a-zA-Z0-9_.+-]+)`

	versionRegex         = regexp.MustCompile(`^` + versionNum + `\.` + versionNum + `\.` + versionNum + internalPatchVersion + "?$")
	gkeExtraVersionRegex = regexp.MustCompile(`^(?:gke)\.(0|[1-9][0-9]*)$`)
)

type version struct {
	version [4]int
}

func (v *version) String() string {
	if v.version[3] != -1 {
		return fmt.Sprintf("%d.%d.%d-gke.%d", v.version[0], v.version[1], v.version[2], v.version[3])
	}

	return fmt.Sprintf("%d.%d.%d", v.version[0], v.version[1], v.version[2])
}

func (v *version) major() int {
	return v.version[0]
}

func (v *version) minor() int {
	return v.version[1]
}

func (v *version) patch() int {
	return v.version[2]
}

func (v *version) extra() int {
	return v.version[3]
}

func (v *version) isGKEExtraVersion(extrastr string) bool {
	return gkeExtraVersionRegex.MatchString(extrastr)
}

func extractGKEExtraVersion(extra string) (int, error) {
	m := gkeExtraVersionRegex.FindStringSubmatch(extra)
	if len(m) != 2 {
		return -1, fmt.Errorf("invalid GKE Patch version %q", extra)
	}

	v, err := strconv.Atoi(m[1])
	if err != nil {
		return -1, fmt.Errorf("GKE extra version atoi failed: %q", extra)
	}

	if v < 0 {
		return -1, fmt.Errorf("GKE extra version check failed: %q", extra)
	}

	return v, nil
}

func parseVersion(vs string) (*version, error) {
	// If version has a prefix 'v', remove it before parsing.
	vs = strings.TrimPrefix(vs, "v")

	submatches := versionRegex.FindStringSubmatch(vs)
	if submatches == nil {
		return nil, fmt.Errorf("version %q is invalid", vs)
	}

	var v version
	// submatches[0] is the whole match, [1]..[3] are the version bits, [4] is the extra
	for i, sm := range submatches[1:4] {
		var err error
		if v.version[i], err = strconv.Atoi(sm); err != nil {
			return nil, fmt.Errorf("submatch %q failed atoi conversion", sm)
		}
	}

	// Ensure 1.X.Y < 1.X.Y-gke.0
	v.version[3] = -1
	if submatches[4] != "" {
		extrastr := submatches[4][1:]
		if v.isGKEExtraVersion(extrastr) {
			ver, err := extractGKEExtraVersion(extrastr)
			if err != nil {
				return nil, err
			}
			v.version[3] = ver
		} else {
			return nil, fmt.Errorf("GKE extra version check failed: %q", extrastr)
		}
	}

	return &v, nil
}

// mustParseVersion parses a GKE cluster version.
func mustParseVersion(version string) *version {
	v, err := parseVersion(version)
	if err != nil {
		klog.Fatalf("Failed to parse GKE version: %q", version)
	}

	return v
}

// Helper function to compare versions.
//
//	-1 -- if left  < right
//	 0 -- if left == right
//	 1 -- if left  > right
func (v *version) compare(right *version) int {
	for i, b := range v.version {
		if b > right.version[i] {
			return 1
		}
		if b < right.version[i] {
			return -1
		}
	}

	return 0
}

// Compare versions if left is strictly less than right.
func (v *version) lessThan(right *version) bool {
	return v.compare(right) < 0
}

// Compare versions if left is greater than or equal to right.
func (v *version) atLeast(right *version) bool {
	return v.compare(right) >= 0
}
