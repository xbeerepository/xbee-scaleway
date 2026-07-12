package scaleway

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/iodasolutions/xbee-common/provider"
)

const (
	envTagPrefix  = "xbee-env-"
	nameTagPrefix = "xbee-name-"
	idTagPrefix   = "xbee-id-"
)

var invalidName = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func scwName(name string) string {
	name = strings.ToLower(invalidName.ReplaceAllString(name, "-"))
	name = strings.Trim(name, "-")
	if name == "" {
		return "xbee"
	}
	return name
}

func envTag() string {
	return envTagPrefix + scwName(provider.EnvId())
}

func nameTag(name string) string {
	return nameTagPrefix + scwName(name)
}

func idTag(hash string) string {
	return idTagPrefix + scwName(hash)
}

func tagsForResource(name string) []string {
	return []string{envTag(), nameTag(name)}
}

func imageNameFor(hash string) string {
	return scwName(fmt.Sprintf("xbee-%s", hash))
}
