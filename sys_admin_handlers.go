package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// IsAdmin checks if the authenticated user has system-wide administrative privileges.
func IsAdmin(c *gin.Context) (bool, error) {
	user, err := getUser(c)
	if err != nil {
		return false, err
	}
	return user.SysAdmin, nil
}

func getYardsAdmin(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Fetch ALL yards via aggregation pass
	yards, err := getAllYardSummaries(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Admin query failed"})
		return
	}

	c.JSON(http.StatusOK, yards)
}

func getNodesAdmin(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Fetch ALL flat network nodes
	nodes, err := getAllNodeSummaries(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Admin query failed"})
		return
	}

	c.JSON(http.StatusOK, nodes)
}

func getCompaniesAdmin(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Fetch ALL companies (passing nil or a flag to bypass filters)
	companies, err := getAllCompanySummaries(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Admin query failed"})
		return
	}

	c.JSON(http.StatusOK, companies)
}

func getUsersAdmin(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Fetch users where sys_admin is NOT true
	users, err := GetNonAdminUsers(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}

	c.JSON(http.StatusOK, users)
}

func getUserDetails(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Parse the ObjectID from the URL
	idParam := c.Param("id")
	userID, err := primitive.ObjectIDFromHex(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format"})
		return
	}

	// 3. Fetch the user
	user, err := GetUserByID(c, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

func editUser(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Parse Target User ID
	idParam := c.Param("id")
	targetID, err := primitive.ObjectIDFromHex(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format"})
		return
	}

	// 3. Bind the incoming Company list
	var input struct {
		CompanyID string `json:"company_id"`
		Remove    bool   `json:"remove"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// 4. Update the User in MongoDB
	err = UpdateUserCompanies(c, targetID, input.CompanyID, input.Remove)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database update failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User companies updated successfully"})
}

func deleteUser(c *gin.Context) {
	// 1. Security Check
	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. SysAdmin only."})
		return
	}

	// 2. Parse Target User ID
	idParam := c.Param("id")
	targetID, err := primitive.ObjectIDFromHex(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format"})
		return
	}

	// 3. Delete the User from MongoDB
	err = DeleteUserByID(c, targetID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database delete failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}
