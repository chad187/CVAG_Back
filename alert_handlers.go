package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func getAlert(c *gin.Context) {
	yardID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, yardID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	alert, err := getAlertImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load alert details:" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, alert)
}

func postAlert(c *gin.Context) {

	yardID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, yardID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	err = postAlertImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update alert details" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert details updated successfully"})
}

func addUserAlert(c *gin.Context) {

	yardID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, yardID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	err = addUserAlertImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to add user alert details:" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User alert details added successfully"})
}

func deleteUserAlert(c *gin.Context) {

	yardID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, yardID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	err = deleteUserAlertImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to delete user alert details: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User alert details deleted successfully"})
}

func deleteAlertHistory(c *gin.Context) {

	yardID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, yardID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	err = deleteAlertHistoryImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to delete alert history: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert history deleted successfully"})
}

func broadcastAlert(c *gin.Context) {
	yardID := c.Param("id")

	// Check if the request is coming from an IoT device using query or form parameters with a password
	devicePassword := c.Query("password")
	if devicePassword == "" {
		devicePassword = c.PostForm("password")
	}

	if devicePassword != "" {
		// IoT Device Authentication Path
		expectedPassword := os.Getenv("API_PASSWORD")
		if devicePassword != expectedPassword {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API password"})
			return
		}
		// Optional: Verify if the IoT device's associated node matches the yardID if needed
	} else {
		// Frontend User Authentication Path (JWT / Bearer Token)
		allowed, err := CanAccessNode(c, yardID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
			return
		}
	}

	// Execute standard broadcast implementation
	remaining, err := broadcastAlertImpl(c, yardID, false)
	if err != nil {
		// check if it has the words "rate limit"
		if strings.Contains(err.Error(), "rate limit") {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"status":            "error",
				"minutes_remaining": fmt.Sprintf("%.1f", remaining),
				"message":           "Rate limit exceeded. wait" + fmt.Sprintf(" %.1f minutes before sending another alert", remaining),
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to broadcast alert: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert broadcast successfully"})
}

func testAlertOne(c *gin.Context) {

	yardID := c.Param("id")

	devicePassword := c.Query("password")
	if devicePassword == "" {
		devicePassword = c.PostForm("password")
	}

	if devicePassword != "" {
		// IoT Device Authentication Path
		expectedPassword := os.Getenv("API_PASSWORD")
		if devicePassword != expectedPassword {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API password"})
			return
		}
		// Optional: Verify if the IoT device's associated node matches the yardID if needed
	} else {
		// Frontend User Authentication Path (JWT / Bearer Token)
		allowed, err := CanAccessNode(c, yardID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
			return
		}
	}

	err := testAlertOneImpl(c, yardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to test one alert: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert test one completed successfully"})
}

func testAlertAll(c *gin.Context) {

	yardID := c.Param("id")

	devicePassword := c.Query("password")
	if devicePassword == "" {
		devicePassword = c.PostForm("password")
	}

	if devicePassword != "" {
		// IoT Device Authentication Path
		expectedPassword := os.Getenv("API_PASSWORD")
		if devicePassword != expectedPassword {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API password"})
			return
		}
		// Optional: Verify if the IoT device's associated node matches the yardID if needed
	} else {
		// Frontend User Authentication Path (JWT / Bearer Token)
		allowed, err := CanAccessNode(c, yardID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
			return
		}
	}

	remaining, err := broadcastAlertImpl(c, yardID, true)
	if err != nil {
		if strings.Contains(err.Error(), "rate limit") {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"status":            "error",
				"minutes_remaining": fmt.Sprintf("%.1f", remaining),
				"message":           fmt.Sprintf("Rate limit exceeded. Wait %.1f minutes before sending another alert", remaining),
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to test all alerts: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert test all completed successfully"})
}
