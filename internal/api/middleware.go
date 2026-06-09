package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = "req_" + uuid.New().String()
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}
