package preparation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CatalogHTTPHandler exposes the anonymous, read-only catalog REST surface.
type CatalogHTTPHandler struct {
	catalog CatalogReader
}

func NewCatalogHTTPHandler(catalog CatalogReader) *CatalogHTTPHandler {
	if catalog == nil {
		panic("preparation catalog reader is required")
	}
	return &CatalogHTTPHandler{catalog: catalog}
}

func (h *CatalogHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/scenario-definitions", h.listScenarios)
	routes.GET("/v1/scenario-definitions/:scenario_definition_id", h.getScenario)
	routes.GET(
		"/v1/scenario-definitions/:scenario_definition_id/role-definitions",
		h.listRoles,
	)
}

func (h *CatalogHTTPHandler) listScenarios(c *gin.Context) {
	definitions := h.catalog.ListActiveScenarios()
	scenarios := make([]ScenarioDefinitionListItem, 0, len(definitions))
	for _, definition := range definitions {
		detail, err := h.catalog.GetScenarioDetail(definition.ID)
		if err != nil {
			writeCatalogError(c, err)
			return
		}
		scenarios = append(scenarios, ScenarioDefinitionListItem{
			ScenarioDefinition: definition,
			Summary:            detail.ScenarioConfig.PromptModel.PublicSceneBrief,
		})
	}
	c.JSON(http.StatusOK, ScenarioDefinitionList{
		Scenarios: scenarios,
	})
}

func (h *CatalogHTTPHandler) getScenario(c *gin.Context) {
	result, err := h.catalog.GetScenarioDetail(
		c.Param("scenario_definition_id"),
	)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CatalogHTTPHandler) listRoles(c *gin.Context) {
	roles, err := h.catalog.ListRoles(c.Param("scenario_definition_id"))
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, RoleDefinitionList{Roles: roles})
}

func writeCatalogError(c *gin.Context, err error) {
	if errors.Is(err, ErrScenarioDefinitionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":           "scenario_definition_not_found",
				"message":        "Scenario definition was not found.",
				"retryable":      false,
				"correlation_id": newCatalogCorrelationID(),
			},
		})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"code":           "internal_error",
			"message":        "Internal server error.",
			"retryable":      false,
			"correlation_id": newCatalogCorrelationID(),
		},
	})
}

func newCatalogCorrelationID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr_catalog_unavailable"
	}
	return "corr_" + hex.EncodeToString(value[:])
}
