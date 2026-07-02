package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func getYards(c *gin.Context) {
	companyID := c.Param("id")

	// 1. Identify and Fetch User
	// Uses your existing helper to pull the user object from the Gin context
	user, err := getUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: " + err.Error()})
		return
	}

	// 2. STRICT ACCESS CHECK
	// Verifying the requested companyID is in the user's slice
	isAuthorized := false
	for _, id := range user.CompanyIDs {
		if id == companyID {
			isAuthorized = true
			break
		}
	}

	if !isAuthorized {
		// Essential logging for your security audits
		log.Printf("SECURITY ALERT: User %s unauthorized access attempt to company %s", user.Email, companyID)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this company"})
		return
	}

	// 3. FETCH SUMMARIES (The Deep Dive)
	// Uses the new helper to get node counts and health status
	summaries, err := getYardSummaries(c, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to aggregate yard data"})
		return
	}

	c.JSON(http.StatusOK, summaries)
}
