package scaleway

import (
	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/provider"
)

type ScalewayVolumeData struct {
	ProjectId  string `yaml:"projectId,omitempty"`
	Zone       string `yaml:"zone,omitempty"`
	VolumeType string `yaml:"volumeType,omitempty"`
}

type Volume struct {
	*provider.XbeeVolume
	Specification *ScalewayVolumeData
}

func volumeFrom(req *provider.XbeeVolume) (*Volume, *cmd.XbeeError) {
	var m ScalewayVolumeData
	if req.Provider != nil && req.Provider.Node() != nil {
		if err := req.Provider.Node().Decode(&m); err != nil {
			return nil, cmd.Error("cannot unmarshal scaleway provider data for volume %s : %v", req.Name, err)
		}
	}
	return &Volume{XbeeVolume: req, Specification: &m}, nil
}

func VolumesFrom() (map[string]map[string]*Volume, *cmd.XbeeError) {
	volumes := provider.VolumesForEnv()
	result := map[string]map[string]*Volume{}
	for _, vReq := range volumes {
		v, err := volumeFrom(vReq)
		if err != nil {
			return nil, err
		}
		if _, ok := result[v.Specification.Zone]; !ok {
			result[v.Specification.Zone] = map[string]*Volume{}
		}
		result[v.Specification.Zone][v.Name] = v
	}
	return result, nil
}
