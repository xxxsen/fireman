package service

import (
	"github.com/fireman/fireman/internal/repository"
)

func applyScenarioCopyDefaults(req *ScenarioCreateRequest, src repository.AllocationScenario) {
	req.Weights = src.Weights
	req.RegionTargets = src.RegionTargets
	if req.Name == "" {
		req.Name = src.Name + " (副本)"
	}
	if req.Description == "" {
		req.Description = src.Description
	}
}
