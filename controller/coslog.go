package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/pkg/coslog"
	"github.com/gin-gonic/gin"
)

func GetCosLogStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    coslog.GetStatus(),
	})
}

func ResetCosLogDropped(c *gin.Context) {
	previous := coslog.ResetDroppedTotal()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"previous": previous,
		},
	})
}
