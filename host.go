package scaleway

import (
	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/provider"
)

type ScalewayHostData struct {
	ProjectId      string `yaml:"projectId,omitempty"`
	Zone           string `yaml:"zone,omitempty"`
	CommercialType string `yaml:"commercialType,omitempty"`
	Image          string `yaml:"image,omitempty"`
	VolumeType     string `yaml:"volumeType,omitempty"`
	Size           int    `yaml:"size,omitempty"`
}

type ProviderHost struct {
	*provider.XbeeHost
	Specification *ScalewayHostData
}

func hostFrom(req *provider.XbeeHost) (*ProviderHost, *cmd.XbeeError) {
	var m ScalewayHostData
	if req.Provider != nil && req.Provider.Node() != nil {
		if err := req.Provider.Node().Decode(&m); err != nil {
			return nil, cmd.Error("cannot unmarshal scaleway provider data for host %s : %v", req.Name, err)
		}
	}
	if m.Image == "" {
		images, ok := provider.SystemProviderDataFor(req.SystemHash)["image"].(map[string]interface{})
		if ok {
			if imageForArch, ok := images[req.OsArch].(map[string]interface{}); ok {
				if image, ok := imageForArch["id"].(string); ok {
					m.Image = image
				}
				if image, ok := imageForArch["label"].(string); ok && m.Image == "" {
					m.Image = image
				}
			}
		}
	}
	return &ProviderHost{XbeeHost: req, Specification: &m}, nil
}

func HostsByZone() (map[string]map[string]*ProviderHost, *cmd.XbeeError) {
	hosts := provider.Hosts()
	result := map[string]map[string]*ProviderHost{}
	for _, hReq := range hosts {
		h, err := hostFrom(hReq)
		if err != nil {
			return nil, err
		}
		if _, ok := result[h.Specification.Zone]; !ok {
			result[h.Specification.Zone] = map[string]*ProviderHost{}
		}
		result[h.Specification.Zone][h.Name] = h
	}
	return result, nil
}
