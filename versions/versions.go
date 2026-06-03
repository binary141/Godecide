package versions

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrVersionNotSupported = errors.New("version not implemented")
var ErrVersionNotFound = errors.New("version not found")

const MaxSupportedVersion = "1.3"

func DetectVersion(namespace string) (string, error) {
	var namespaceVersions = map[string]string{
		"https://www.omg.org/spec/DMN/20240513/MODEL/": "1.6",
		"https://www.omg.org/spec/DMN/20230324/MODEL/": "1.5",
		"https://www.omg.org/spec/DMN/20211108/MODEL/": "1.4",
		"https://www.omg.org/spec/DMN/20191111/MODEL/": "1.3",
		"http://www.omg.org/spec/DMN/20180521/MODEL/":  "1.2",
		"http://www.omg.org/spec/DMN/20151101/dmn.xsd": "1.1",
		"http://www.omg.org/spec/DMN/20130901":         "1.0",
	}

	version, ok := namespaceVersions[namespace]
	if !ok {
		return "", ErrVersionNotFound
	}

	v, err := ParseVersion(version)
	if err != nil {
		return "", err
	}

	maxVersion, err := ParseVersion(MaxSupportedVersion)
	if err != nil {
		return "", err
	}

	if !v.AtLeast(maxVersion) {
		return "", ErrVersionNotFound
	}

	return version, nil
}

type Version struct {
	Major int
	Minor int
}

func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return Version{}, fmt.Errorf("invalid version: %s", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version: %s", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	return Version{Major: major, Minor: minor}, nil
}

func (v Version) AtLeast(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	return v.Minor >= other.Minor
}
