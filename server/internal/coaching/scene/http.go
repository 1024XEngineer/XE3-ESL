package scene

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
		panic("scene catalog reader is required")
	}
	return &CatalogHTTPHandler{catalog: catalog}
}

func (h *CatalogHTTPHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/v1/scenes", h.listScenes)
	routes.GET("/v1/scenes/:scene_id", h.getScene)
	routes.GET(
		"/v1/scenes/:scene_id/roles",
		h.listRoles,
	)
	if _, ok := h.catalog.(IELTSQuestionBankReader); ok {
		routes.GET(
			"/v1/scenes/ielts-speaking/question-bank",
			h.getIELTSQuestionBank,
		)
	}
}

func (h *CatalogHTTPHandler) listScenes(c *gin.Context) {
	definitions, err := h.catalog.ListActiveScenes(c.Request.Context())
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scenes": definitions})
}

func (h *CatalogHTTPHandler) getScene(c *gin.Context) {
	result, err := h.catalog.GetScene(
		c.Request.Context(),
		c.Param("scene_id"),
	)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CatalogHTTPHandler) listRoles(c *gin.Context) {
	roles, err := h.catalog.ListRoles(
		c.Request.Context(),
		c.Param("scene_id"),
	)
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

func (h *CatalogHTTPHandler) getIELTSQuestionBank(c *gin.Context) {
	reader, ok := h.catalog.(IELTSQuestionBankReader)
	if !ok {
		writeCatalogError(c, ErrIELTSQuestionBankUnavailable)
		return
	}
	bank, err := reader.IELTSQuestionBank()
	if err != nil {
		writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, bank)
}

func writeCatalogError(c *gin.Context, err error) {
	if errors.Is(err, ErrSceneNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":           "scene_not_found",
				"message":        "Scene was not found.",
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
