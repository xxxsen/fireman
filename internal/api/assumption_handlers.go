package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
)

func (s Services) registerAssumptionRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/simulation-assumptions")
	g.GET("/profiles", s.listAssumptionProfiles)
	g.POST("/profiles", s.createAssumptionProfile)
	g.GET("/profiles/:id/:version", s.getAssumptionProfile)
	g.POST("/profiles/:id/:version/validate", s.validateAssumptionProfile)
	g.POST("/profiles/:id/:version/activate", s.activateAssumptionProfile)
	g.GET("/preferences", s.getAssumptionPreferences)
	g.PUT("/preferences", s.setAssumptionPreferences)
}

func (s Services) listAssumptionProfiles(c *gin.Context) {
	out, err := s.Assumptions.ListProfiles(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getAssumptionProfile(c *gin.Context) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", "version must be an integer", nil)
		return
	}
	out, err := s.Assumptions.GetProfile(c.Request.Context(), c.Param("id"), version)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"profile": out})
}

type assumptionProfileRequest struct {
	Profile    assumptions.Profile `json:"profile"`
	SourceNote string              `json:"source_note"`
	ReviewedBy string              `json:"reviewed_by"`
	ReviewedAt string              `json:"reviewed_at"`
}

func (s Services) createAssumptionProfile(c *gin.Context) {
	var req assumptionProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Assumptions.SaveDraft(
		c.Request.Context(), req.Profile, req.SourceNote, req.ReviewedBy, req.ReviewedAt,
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"profile": out})
}

func (s Services) validateAssumptionProfile(c *gin.Context) {
	var req assumptionProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	// A non-system profile must never claim a reserved system_cma_ id (td/067 R13).
	if req.Profile.OwnerScope != assumptions.OwnerSystem && assumptions.HasReservedSystemID(req.Profile.ID) {
		Fail(c, http.StatusBadRequest, "assumption_profile_reserved_id",
			"profile id uses the reserved 'system_cma_' namespace; user profiles receive a server-assigned id",
			map[string]any{"id": req.Profile.ID})
		return
	}
	OK(c, s.Assumptions.ValidateProfile(req.Profile))
}

func (s Services) activateAssumptionProfile(c *gin.Context) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", "version must be an integer", nil)
		return
	}
	if err := s.Assumptions.Activate(c.Request.Context(), c.Param("id"), version); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"activated": true})
}

func (s Services) getAssumptionPreferences(c *gin.Context) {
	out, err := s.Assumptions.GetPreferences(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"preferences": out})
}

func (s Services) setAssumptionPreferences(c *gin.Context) {
	var req struct {
		Preferences repository.AssumptionPreferences `json:"preferences"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Assumptions.SetPreferences(c.Request.Context(), req.Preferences)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"preferences": out})
}
