package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/quickfire"
)

const quickFireRequestMaxBytes = 32 << 10

var errQuickFireSingleJSONObject = errors.New("request must contain exactly one JSON object")

func (s Services) registerQuickFIRERoutes(rg *gin.RouterGroup) {
	rg.POST("/fire/quick-calculations", s.calculateQuickFIRE)
}

func (s Services) calculateQuickFIRE(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, quickFireRequestMaxBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input quickfire.Input
	if err := decoder.Decode(&input); err != nil {
		quickFireInputError(c, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		quickFireInputError(c, errQuickFireSingleJSONObject)
		return
	}
	result, err := quickfire.Calculate(input)
	if err != nil {
		var validation *quickfire.ValidationError
		switch {
		case errors.As(err, &validation):
			Fail(c, http.StatusBadRequest, "quick_fire_parameters_invalid", "FIRE 快算参数不合法", map[string]any{
				"fields": validation.Fields,
			})
		case errors.Is(err, quickfire.ErrResultOutOfRange):
			Fail(c, http.StatusUnprocessableEntity, "quick_fire_result_out_of_range", "当前参数产生的结果超出可计算范围", map[string]any{
				"field": "projection",
			})
		default:
			Fail(c, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		}
		return
	}
	OK(c, result)
}

func quickFireInputError(c *gin.Context, err error) {
	Fail(c, http.StatusBadRequest, "quick_fire_parameters_invalid", "FIRE 快算参数不合法", map[string]any{
		"fields": map[string]string{"request": err.Error()},
	})
}
