package main

import (
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	KEY_OK                 = "ok"
	KEY_LOW_BATTERY_STATUS = "low_battery"
	KEY_OFFLINE            = "offline"
	KEY_OVERHEATING        = "overheating"
	KEY_PENDING            = "pending"
	KEY_QUEUED             = "queued"
	KEY_COMPLETED          = "completed"
	KEY_WARNING_TEMP       = 80 // default threshold for overheating
	KEY_LOW_BATTERY        = 20 // default threshold for low battery
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file - make sure it exists!")
	}

	db_name := os.Getenv("DB_NAME")
	if db_name == "" {
		log.Fatal("DB_NAME environment variable is required")
	}

	initDB(db_name) // Connect to MongoDB and set up indexes
	initAuth()
	r := setupRouter()
	backPort := os.Getenv("BACK_PORT")
	if backPort == "" {
		log.Fatal("BACK_PORT environment variable is required")
	}
	r.Run(backPort) // Listen on Pi port 8080

}

func setupRouter() *gin.Engine {
	r := gin.Default()

	// --- CORS CONFIGURATION START ---
	// This MUST come before your routes
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{frontendBaseURL}, // Your Vite URL
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	// --- CORS CONFIGURATION END ---

	// --- LOGGING MIDDLEWARE ---
	r.Use(func(c *gin.Context) {
		startTime := time.Now().UTC()
		method := c.Request.Method
		path := c.Request.RequestURI

		// Process request
		c.Next()

		// Log after request completes
		duration := time.Since(startTime)
		statusCode := c.Writer.Status()
		log.Printf("[%s] %s - Status: %d - Duration: %v", method, path, statusCode, duration)
	})
	// --- LOGGING MIDDLEWARE END ---

	api := r.Group("/api")
	{
		// auth routes
		// GET /auth/google/login
		// Summary: Redirect user to Google OAuth login.
		// Responses: 302 redirect to Google OAuth consent.
		api.GET("/auth/google/login", googleLogin)

		// GET /auth/google/callback
		// Summary: Receive Google OAuth callback and issue JWT.
		// Query params: state, code
		// Responses: 302 redirect to frontend with token or 400/500 on failure.
		api.GET("/auth/google/callback", googleCallback)

		// GET /auth/microsoft/login
		// Summary: Redirect user to Microsoft OAuth login.
		// Responses: 302 redirect to Microsoft OAuth consent.
		api.GET("/auth/microsoft/login", microsoftLogin)

		// GET /auth/microsoft/callback
		// Summary: Receive Microsoft OAuth callback and issue JWT.
		// Query params: state, code
		// Responses: 302 redirect to frontend with token or 400/500 on failure.
		api.GET("/auth/microsoft/callback", microsoftCallback)

		// GET /auth/me
		// Summary: Return the authenticated user's profile.
		// Header: Authorization: Bearer <token>
		// Responses: 200 User object, 401 invalid/missing token, 404 user not found.
		api.GET("/auth/me", authMe)

		// sys admin routes
		// GET /admin/companies
		// Summary: Return summaries for all companies (SysAdmin only).
		// Header: Authorization: Bearer <token>
		// Responses: 200 list of CompanySummary, 401/403 on auth failure.
		api.GET("/admin/companies", getCompaniesAdmin)

		api.GET("/admin/yards", getYardsAdmin)

		api.GET("/admin/nodes", getNodesAdmin)

		// GET /admin/users
		// Summary: Return all non-admin users.
		// Header: Authorization: Bearer <token>
		// Responses: 200 list of User, 401/403 on auth failure.
		api.GET("/admin/users", getUsersAdmin)

		// GET /admin/user/:id
		// Summary: Return a single user by ID.
		// Path param: id (user ObjectID)
		// Header: Authorization: Bearer <token>
		// Responses: 200 User, 400 invalid ID, 401/403 auth failure, 404 not found.
		api.GET("/admin/user/:id", getUserDetails)

		// POST /admin/user/:id
		// Summary: Update a user's company access list.
		// Path param: id (user ObjectID)
		// Header: Authorization: Bearer <token>
		// Body: { "company_ids": ["COMP_A", "COMP_B"] }
		// Responses: 200 success, 400 invalid payload, 401/403 auth failure, 404 not found.
		api.POST("/admin/user/:id", editUser)

		// GET /company/:id/yards
		// Summary: Return yard summaries for a company.
		// Path param: id (company ID)
		// Header: Authorization: Bearer <token>
		// Responses: 200 list of YardSummary, 401/403 unauthorized.
		api.GET("/company/:id/yards", getYards)

		// GET /yard/:id/nodes
		// Summary: Return nodes for a yard.
		// Path param: id (yard ID)
		// Header: Authorization: Bearer <token>
		// Responses: 200 list of Node, 401/403 unauthorized.
		api.GET("/yard/:id/nodes", getNodes)

		// GET /nodes/:id
		// Summary: Return a single node summary.
		// Path param: id (node ID)
		// Header: Authorization: Bearer <token>
		// Responses: 200 NodeSummary, 401/403 unauthorized, 404 not found.
		api.GET("/nodes/:id", getNode)

		// PUT /node/:id/details
		// Summary: Update node details like name and warning temperature.
		// Path param: id (node ID)
		// Header: Authorization: Bearer <token>
		// Body: Node JSON with updated fields (name, warning_temp)
		// Responses: 200 success, 400 invalid payload, 401/403 unauthorized, 404 not found.
		api.PUT("/nodes/:id/details", updateNodeDetails)

		// DELETE /update/:id
		// Summary: Cancel a pending or queued firmware update for a node.
		// Path param: id (node ID)
		// Header: Authorization: Bearer <token>
		// Responses: 200 success, 401/403 unauthorized, 404 not found, 409 conflict.
		api.DELETE("/update/:id", deleteUpdate)

		// POST /update/:id
		// Summary: Queue a firmware update for a node.
		// Path param: id (node ID)
		// Header: Authorization: Bearer <token>
		// Body: FirmwareUpdate payload
		// Responses: 200 update accepted, 401/403 unauthorized, 400 invalid payload, 404 not found.
		api.POST("/update/:id", queueUpdate)

		// POST /update
		// Summary: Queue a firmware update for all nodes in a yard.
		// Path param: id (node ID)
		// Header: Authorization: Bearer <token>
		// Body: FirmwareUpdate payload
		// Responses: 200 update accepted, 401/403 unauthorized, 400 invalid payload, 404 not found.
		api.POST("/updates", queueBatchUpdates)

		// GET /yard/:id/alert
		// Summary: Return alert control metadata and current user details.
		// Header: Authorization: Bearer <token>
		// Responses: 200 AlertPayload, 401 unauthorized.
		api.GET("/yard/:id/alert", getAlert)

		// POST /yard/:id/alert
		// Summary: Accept Alert message payload.
		// Header: Authorization: Bearer <token>
		// Body: { message, last_run, cool_down, test_email, test_phone, run_history }
		// Responses: 200 success, 401/403 unauthorized.
		api.POST("/yard/:id/alert", postAlert)

		// PUT /yard/:id/alert/user
		// Summary: Accept user payload.
		// Header: Authorization: Bearer <token>
		// Body: { name, email, phone, language }
		// Responses: 200 success, 401/403 unauthorized.
		api.PUT("/yard/:id/alert/user", addUserAlert)

		// DELETE /yard/:id/alert/user
		// Summary: delete user payload.
		// Header: Authorization: Bearer <token>
		// Body: { email }
		// Responses: 200 success, 401/403 unauthorized.
		api.DELETE("/yard/:id/alert/user", deleteUserAlert)

		// DELETE /yard/:id/alert/history
		// Summary: delete Alert history.
		// Header: Authorization: Bearer <token>
		// Body: { date }
		// Responses: 200 success, 401/403 unauthorized.
		api.DELETE("/yard/:id/alert/history/:date", deleteAlertHistory)

		// POST /yard/:id/alert/broadcast
		// Summary: Broadcast alert message to all users in the yard.
		// Header: Authorization: Bearer <token>
		// Responses: 200 success, 401/403 unauthorized.
		api.POST("/yard/:id/alert/broadcast", broadcastAlert)

		// POST /yard/:id/alert/testAll
		// Summary: Broadcast alert message to the test recipients.
		// Header: Authorization: Bearer <token>
		// Responses: 200 success, 401/403 unauthorized.
		api.POST("/yard/:id/alert/testAll", testAlertAll)

		// POST /yard/:id/alert/testOne
		// Summary: Broadcast alert message to the test recipients.
		// Header: Authorization: Bearer <token>
		// Responses: 200 success, 401/403 unauthorized.
		api.POST("/yard/:id/alert/testOne", testAlertOne)

		// POST /heartbeat
		// Summary: Process node heartbeat data from remote/local gateway.
		// Body: HeartbeatPayload JSON
		// Responses: 200 success, 400 invalid payload.
		api.POST("/heartbeat", handleHeartbeat)

		// POST /metrics
		// Summary: recieve gateway info
		// Body: SystemMetricsPayload JSON
		// Responses: 200 success, 400 invalid payload.
		api.POST("/metrics", handleMetrics)

		// GET /metrics/:id
		// Path param: id (yard ID)
		// Summary: send gateway info
		// Body: SystemMetricsPayload JSON
		// Responses: 200 success, 400 invalid payload.
		api.GET("/metrics/:id", getMetrics)

		api.GET("firmwareUpdate/:id", deliverFirmware)
	}

	return r
}
