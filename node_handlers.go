package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func getNodes(c *gin.Context) {
	yardID := c.Param("id")
	user, err := getUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	compositeID, err := BreakoutCompositeID(yardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Hierarchical Access Check
	if !slices.Contains(user.CompanyIDs, compositeID.CompanyID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Fetch nodes for the specified yard
	nodes, err := GetNodesByYardID(c, yardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nodes"})
		return
	}

	c.JSON(http.StatusOK, nodes)
}

func getNode(c *gin.Context) {
	nodeID := c.Param("id")
	user, err := getUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Hierarchical Access Check
	parts := strings.Split(nodeID, ":")
	if len(parts) < 3 || !slices.Contains(user.CompanyIDs, parts[0]) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Use the helper!
	summary, err := GetNodeSummary(c, nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	c.JSON(http.StatusOK, summary)
}

func GetNodeSummary(ctx context.Context, nodeID string) (NodeSummary, error) {
	var summary NodeSummary

	// 1. Fetch the Node
	err := database.Collection("nodes").FindOne(ctx, bson.M{"_id": nodeID}).Decode(&summary.Node)
	if err != nil {
		return summary, err
	}

	// 2. Fetch recent history (Last 100 entries)
	opts := options.Find().SetSort(bson.M{"timestamp": -1}).SetLimit(100) //this might have to be extented to get more history for the graph
	cursor, err := database.Collection("history").Find(ctx, bson.M{"node_id": nodeID}, opts)
	if err == nil {
		defer cursor.Close(ctx)
		cursor.All(ctx, &summary.History)
	}

	// 3. Check for pending/queued updates
	var update FirmwareUpdate
	projection := bson.M{
		"firmware": 0,
		"node_id":  0,
	}

	err = database.Collection("updates").FindOne(ctx, bson.M{
		"_id":           nodeID,
		"update_status": bson.M{"$in": bson.A{KEY_PENDING, KEY_QUEUED}},
	}, options.FindOne().SetProjection(projection)).Decode(&update)
	summary.Update = update

	return summary, nil
}

func queueUpdate(c *gin.Context) {
	nodeID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, nodeID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this node"})
		return
	}

	// 1. Initialize your model variable

	// 2. Parse firmware update payload
	var update FirmwareUpdate
	if err := c.ShouldBind(&update); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid form fields provided"})
		return
	}

	// 3. Extract the raw file header from the multi-part stream using your React key ('firmware')
	fileHeader, err := c.FormFile("firmware")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Firmware binary file is required"})
		return
	}

	// 4. Open the data stream pointer
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open firmware file stream"})
		return
	}
	defer file.Close()

	// 5. Read the incoming binary stream directly into your update.Firmware []byte buffer
	update.Firmware, err = io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read binary stream data"})
		return
	}

	// 3. Create and queue the firmware update
	result, err := CreateFirmwareUpdate(c, nodeID, update)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error:": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    result.UpdateStatus,
		"expect_at": result.UpdateAt.Format("3:04 PM"),
	})
}

func queueBatchUpdates(c *gin.Context) {
	// 1. Bind form metadata fields (SSID, Password, Version, UpdateUrl, etc.)
	var baseUpdate FirmwareUpdate
	if err := c.ShouldBind(&baseUpdate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid form fields provided"})
		return
	}

	// 2. Extract the list of targeted Node IDs from the form data
	// Expecting a comma-separated string like: "id1,id2,id3" or a stringified JSON array
	nodeIDsInput := c.PostForm("node_ids")
	if nodeIDsInput == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A list of target node_ids is required"})
		return
	}

	// Simple parse assuming standard comma separation
	// If you send a JSON array string like '["id1","id2"]', use json.Unmarshal instead
	var nodeIDs []string
	for _, id := range strings.Split(nodeIDsInput, ",") {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			nodeIDs = append(nodeIDs, trimmed)
		}
	}

	if len(nodeIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid node IDs found in payload"})
		return
	}

	// 3. Bulk Security/Access Validation
	// Loop through early to prevent partial failures halfway through processing
	for _, id := range nodeIDs {
		allowed, err := CanAccessNode(c, id)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized validation checks"})
			return
		}
		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Access denied to node: %s", id)})
			return
		}
	}

	// 4. Extract and read the binary file stream exactly ONCE into memory
	fileHeader, err := c.FormFile("firmware")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Firmware binary file is required"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open firmware file stream"})
		return
	}
	defer file.Close()

	firmwareBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read binary stream data"})
		return
	}

	// Attach the single in-memory buffer directly to our template struct
	baseUpdate.Firmware = firmwareBytes

	// 5. Create and queue updates across all specified nodes
	// Note: Depending on your database driver, you should optimize `CreateFirmwareUpdate`
	// internally to perform a bulk batch-insert or bulk-update query rather than 100 separate executions.
	var results []gin.H

	for _, id := range nodeIDs {
		result, err := CreateFirmwareUpdate(c, id, baseUpdate)
		if err != nil {
			// If a specific node isn't found or fails database state changes, log it and keep moving,
			// or handle transactional rollbacks depending on how strict your system requirements are.
			continue
		}

		results = append(results, gin.H{
			"node_id":   id,
			"status":    result.UpdateStatus,
			"expect_at": result.UpdateAt.Format("3:04 PM"),
		})
	}

	// 6. Return comprehensive tracking array back upstream
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Successfully processed batch update for %d nodes", len(results)),
		"updates": results,
	})
}

func handleHeartbeat(c *gin.Context) {
	// 1. Parse and validate heartbeat payload
	var incoming HeartbeatPayload
	if err := c.ShouldBindJSON(&incoming); err != nil || incoming.Current.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// 2. Validate node ID format
	compositeID, err := BreakoutCompositeID(incoming.Current.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid node ID format"})
		return
	}

	// 3. Process the heartbeat
	if err := ProcessHeartbeat(c, incoming, compositeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process heartbeat"})
		return
	}

	// 4. Handle firmware update logic
	checkAndHandleFirmware(c, incoming.Current, compositeID)
}

func handleMetrics(c *gin.Context) {
	var incoming SystemMetrics

	// 1. Bind JSON
	if err := c.ShouldBindJSON(&incoming); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid metrics payload"})
		return
	}

	incoming.UpdatedAt = time.Now()

	// 3. Database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ReplaceOne ensures we keep exactly one record per YardID
	_, err := database.Collection("yard_metrics").ReplaceOne(
		ctx,
		bson.M{"_id": incoming.YardID},
		incoming,
		options.Replace().SetUpsert(true),
	)

	if err != nil {
		log.Printf("Failed to update metrics: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Persistence failure"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

func updateNodeDetails(c *gin.Context) {
	nodeID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, nodeID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// 2. Bind JSON Request Data
	var input Node
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid details payload"})
		return
	}

	// 3. Delegate to Business Logic Helper
	err = EditNodeDetails(c, nodeID, input)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update node details"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Node metadata updated successfully"})
}

func deleteUpdate(c *gin.Context) {
	nodeID := c.Param("id")

	// 1. Security Check
	allowed, err := CanAccessNode(c, nodeID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// 2. Attempt to cancel the pending update
	result, err := CancelPendingUpdate(c, nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// 3. Handle specific results
	if result.Cancelled {
		c.JSON(http.StatusOK, gin.H{"message": "Pending update successfully cancelled"})
		return
	}

	if result.NotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "No update found for this node"})
		return
	}

	if result.AlreadyQueued {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "Cannot delete update",
			"reason": "Update is already queued on the local gateway and may have been transmitted.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pending update successfully cancelled"})
}

func checkAndHandleFirmware(c *gin.Context, current Node, compositeID CompositeID) {
	// 1. SUCCESS: Mark as completed if the version now matches
	res, _ := database.Collection("updates").UpdateOne(c,
		bson.M{"_id": current.ID, "version": current.Version, "update_status": KEY_QUEUED},
		bson.M{"$set": bson.M{"update_status": KEY_COMPLETED}},
	)

	if res.ModifiedCount > 0 {
		c.JSON(http.StatusOK, gin.H{"has_update": false, "status": "update_completed"})
		return
	}

	// 2. MISS/STALL: If still queued but version hasn't changed, heartbeat happened
	// but update didn't. Just bump the predicted time for the dashboard.
	interval := GetPredictedInterval(c, current)
	nextWindow := current.UpdatedAt.Add(interval)

	//a queued may not exist
	database.Collection("updates").UpdateOne(c,
		bson.M{"_id": current.ID, "update_status": KEY_QUEUED},
		bson.M{"$set": bson.M{"update_at": nextWindow}},
	)
	//pending means that only the remote server has it
	//queued means that the remote server has sent the update to the local server and is waiting for the node to report back with the new version in the heartbeat to mark it as completed
	//completed means that the node has reported back with the new version and the update is done

	// 3. PENDING: Check for brand new updates
	//if you find a queued and then a pending then just overwrite the old queued with the new pending and update the time. This means that the remote server has sent a new update while the old one was still pending, so we just replace it with the new one and reset the timer.
	var update FirmwareUpdate
	err := database.Collection("updates").FindOne(c, bson.M{
		"_id":           current.ID,
		"update_status": KEY_PENDING,
	}).Decode(&update)

	if err == nil {
		if update.UpdateURL == "battery_reset" {
			database.Collection("updates").DeleteOne(c, bson.M{"_id": update.NodeID, "update_status": KEY_PENDING})
		} else {
			database.Collection("updates").UpdateOne(c,
				bson.M{"_id": update.NodeID},
				bson.M{"$set": bson.M{
					"update_status": KEY_QUEUED,
					"update_at":     nextWindow, // Ensure pending moves to queued with a fresh time
				}},
			)
		}

		c.JSON(http.StatusOK, gin.H{
			"has_update": true,
			"target_id":  compositeID.NodeID,
			"ssid":       update.SSID,
			"password":   update.Password,
			"update_url": update.UpdateURL,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"has_update": false, "status": "recorded"})
}

func ensureHierarchyExists(ctx context.Context, idparts CompositeID) {
	// 1. Ensure Company
	database.Collection("companies").UpdateOne(ctx,
		bson.M{"_id": idparts.CompanyID},
		bson.M{"$setOnInsert": Company{
			ID:        idparts.CompanyID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		options.UpdateOne().SetUpsert(true),
	)

	// 2. Ensure Yard
	yardID := idparts.CompanyID + ":" + idparts.YardID
	database.Collection("yards").UpdateOne(ctx,
		bson.M{"_id": yardID},
		bson.M{"$setOnInsert": Yard{
			ID:        yardID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
}

// BreakoutCompositeID takes a node ID in the format "COMPANY:YARD:NODE" and breaks it into its components.
func BreakoutCompositeID(compositeID string) (CompositeID, error) {
	parts := strings.Split(compositeID, ":")
	numParts := len(parts)

	// Validate minimum requirements (Company and Yard)
	if numParts < 2 {
		return CompositeID{}, errors.New("invalid composite ID format: expected at least companyID:yardID")
	}

	res := CompositeID{
		CompanyID: parts[0],
		YardID:    parts[1],
	}

	// If there is a third part, assign it to NodeID
	if numParts == 3 {
		res.NodeID = parts[2]
	}

	if numParts > 3 {
		return CompositeID{}, errors.New("invalid composite ID format: too many parts")
	}

	return res, nil
}

// GetPredictedInterval looks at history to determine how often the node heartbeats.
func GetPredictedInterval(ctx context.Context, node Node) time.Duration {
	var lastHistory HistoryRecord
	opts := options.FindOne().SetSort(bson.M{"created_at": -1})

	err := database.Collection("history").FindOne(ctx, bson.M{"node_id": node.ID}, opts).Decode(&lastHistory)
	if err != nil {
		return 15 * time.Minute // Default fallback
	}

	interval := node.UpdatedAt.Sub(lastHistory.Timestamp)

	// Sanity check for IoT jitter or reboot resets
	if interval <= 0 || interval > 24*time.Hour {
		return 15 * time.Minute
	}
	return interval
}

// UpsertFirmwareUpdate handles the database write logic.
func UpsertFirmwareUpdate(ctx context.Context, update FirmwareUpdate) error {
	opts := options.UpdateOne().SetUpsert(true)
	_, err := database.Collection("updates").UpdateOne(
		ctx,
		bson.M{"_id": update.NodeID},
		bson.M{"$set": update},
		opts,
	)
	return err
}

// CanAccessNode checks if the authenticated user has permission to manage a specific node.
func CanAccessNode(c *gin.Context, nodeID string) (bool, error) {
	user, err := getUser(c)
	if err != nil {
		return false, err
	}

	// Break down the nodeID (e.g., "U0_COMP_0:YARD_0:NODE_1")
	composite, err := BreakoutCompositeID(nodeID)
	if err != nil {
		return false, err
	}

	// Check if the user's authorized companies include the node's company
	if !slices.Contains(user.CompanyIDs, composite.CompanyID) {
		return false, nil
	}

	return true, nil
}

func getMetrics(c *gin.Context) {
	// 1. Identify the requested metric ID (e.g., CompanyID:YardID)
	yardID := c.Param("id")

	admin, err := IsAdmin(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if !admin {
		// 2. Validate User Authentication
		user, err := getUser(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		// 3. Hierarchical Access Check
		// Ensuring the user has access to the company owning this yard
		parts := strings.Split(yardID, ":")
		if len(parts) < 2 || !slices.Contains(user.CompanyIDs, parts[0]) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
	}

	metrics, err := getMetricsHelper(c, yardID)

	// 5. Return the record
	c.JSON(http.StatusOK, metrics)
}

func getMetricsHelper(c *gin.Context, yardID string) (metrics SystemMetrics, err error) {
	// 4. Retrieve metrics from DB
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = database.Collection("yard_metrics").FindOne(ctx, bson.M{"_id": yardID}).Decode(&metrics)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Metrics not found for this yard"})
		return
	}

	return
}

func deliverFirmware(c *gin.Context) {
	nodeID := c.Param("id")

	// 1. Fetch the data using the read-only helper
	updateRecord, err := GetQueuedFirmware(c, nodeID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "No firmware queued"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lookup failed"})
		return
	}

	// 2. Deliver ONLY the raw binary array directly to the socket connection.
	// This ensures the ESP32 reads the first byte of the binary immediately.
	c.Data(http.StatusOK, "application/octet-stream", updateRecord.Firmware)
}
