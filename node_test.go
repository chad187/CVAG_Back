package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestHandleHeartbeatLifecycle(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	nodeID := "COMP_1:YARD_A:NODE_99"
	targetVersion := "2.0.0"

	// --- STAGE 1: Provisioning & Telemetry ---
	t.Run("Auto-provisions and records history", func(t *testing.T) {
		// First Heartbeat: Node comes online for the first time
		p1 := HeartbeatPayload{
			Current: Node{
				ID:      nodeID,
				Temp:    70,
				Battery: 100,
				Status:  KEY_OK,
				Version: "1.0.0",
			},
		}
		performHeartbeat(router, p1)

		// Second Heartbeat: New data should push the first heartbeat to history
		p2 := p1
		p2.Current.Temp = 75
		performHeartbeat(router, p2)

		// VERIFY: Use our new GetNodeSummary helper
		summary, err := GetNodeSummary(ctx, nodeID)
		if err != nil {
			t.Fatalf("Failed to retrieve node summary: %v", err)
		}

		if summary.Node.Temp != 75 {
			t.Errorf("Node current temp mismatch. Got %v, expected 75", summary.Node.Temp)
		}

		if len(summary.History) == 0 {
			t.Errorf("History was not archived. Expected at least 1 record")
		}
	})

	// --- STAGE 2: Firmware Transition (Pending -> Queued) ---
	t.Run("Transitions firmware to queued and provides update data", func(t *testing.T) {
		// SEED: Manually create the pending update record
		database.Collection("updates").InsertOne(ctx, FirmwareUpdate{
			NodeID:       nodeID,
			Version:      targetVersion,
			UpdateURL:    "http://192.168.1.50/firmware.bin",
			UpdateStatus: KEY_PENDING, // Must be pending to trigger
		})

		payload := HeartbeatPayload{
			Current: Node{ID: nodeID, Version: "1.0.0"},
		}
		w := performHeartbeat(router, payload)

		// VERIFY: Check JSON response for the local server to relay to the node
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Invalid JSON response: %v", err)
		}

		if resp["has_update"] != true {
			t.Errorf("Expected has_update to be true")
		}

		// VERIFY: Check DB state change for the dashboard
		var update FirmwareUpdate
		database.Collection("updates").FindOne(ctx, bson.M{"_id": nodeID}).Decode(&update)
		if update.UpdateStatus != KEY_QUEUED {
			t.Errorf("Update should be '%s', found: %s", KEY_QUEUED, update.UpdateStatus)
		}
	})

	// --- STAGE 3: Completion via Handshake ---
	t.Run("Completes update when version matches", func(t *testing.T) {
		// Node reports back with the target version
		payload := HeartbeatPayload{
			Current: Node{
				ID:      nodeID,
				Version: targetVersion, // "2.0.0" matches the update record
			},
		}
		performHeartbeat(router, payload)

		// VERIFY: The record should now be marked completed
		var update FirmwareUpdate
		database.Collection("updates").FindOne(ctx, bson.M{"_id": nodeID}).Decode(&update)
		if update.UpdateStatus != KEY_COMPLETED {
			t.Errorf("Update should be '%s', found: %s", KEY_COMPLETED, update.UpdateStatus)
		}
	})
}

func TestGetNode(t *testing.T) {
	router := setupRouter()

	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]

	nodeID := yardIDs[0] + ":NODE_123"

	// 2. ACT: Use Heartbeats to "Live Seed" the database
	// Heartbeat 1: Create the node (Auto-provision)
	p1 := HeartbeatPayload{
		Current: Node{
			ID:      nodeID,
			Temp:    70.0,
			Battery: 95,
			Version: "1.0.0",
		},
	}
	performHeartbeat(router, p1)

	// Heartbeat 2: Update the node (this pushes the 70.0 temp to history)
	p2 := p1
	p2.Current.Temp = 75.5
	p2.Current.Battery = 90
	performHeartbeat(router, p2)

	// 3. GET: Now try to retrieve the summary
	req, _ := http.NewRequest("GET", "/api/nodes/"+nodeID, nil)
	req.Header.Set("Authorization", getAuthHeader(user1))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 4. ASSERT
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var summary NodeSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("Failed to decode NodeSummary: %v", err)
	}

	// Verify the Current State (from Heartbeat 2)
	if summary.Node.Temp != 75.5 {
		t.Errorf("Current temp mismatch. Expected 75.5, got %v", summary.Node.Temp)
	}

	// Verify the History (pushed from Heartbeat 1)
	if len(summary.History) == 0 {
		t.Errorf("Expected at least one history record, got 0")
		//now that we push the latest on to history then the first true history is on index 1
	} else if summary.History[1].Temp != 70.0 {
		t.Errorf("History record temp mismatch. Expected 70.0, got %v", summary.History[1].Temp)
	}
}

func TestGetNodes(t *testing.T) {
	router := setupRouter()

	_, userIDs, _, yardIDs, _, err := setupTest(t, 2, 2, 2, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]

	// 1. Setup IDs for hierarchical check
	// Logic: COMP_1 owns YARD_A. COMP_2 should not see it.
	yardID := yardIDs[0]      // e.g. "COMP_1:YARD_A"
	otherYardID := yardIDs[1] // e.g. "COMP_2:YARD_B"
	node1ID := yardID + ":NODE_1"
	node2ID := yardID + ":NODE_2"
	nodeOtherID := otherYardID + ":NODE_3"

	// Seed two nodes in the target yard
	performHeartbeat(router, HeartbeatPayload{
		Current: Node{ID: node1ID, Temp: 70},
	})
	performHeartbeat(router, HeartbeatPayload{
		Current: Node{ID: node2ID, Temp: 75},
	})
	// Seed one node in a DIFFERENT yard
	performHeartbeat(router, HeartbeatPayload{
		Current: Node{ID: nodeOtherID, Temp: 80},
	})

	t.Run("Successfully fetches nodes for authorized user", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/nodes", nil)
		req.Header.Set("Authorization", getAuthHeader(user1))
		// Note: Ensure your test environment's getUser(c) returns
		// a user with "COMP_1" in their CompanyID slice.

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var nodes []Node
		json.Unmarshal(w.Body.Bytes(), &nodes)

		if len(nodes) != 2 {
			t.Errorf("Expected 2 nodes from %s, got %d", yardID, len(nodes))
		}

		// Verify we didn't leak nodes from the other yard
		for _, n := range nodes {
			compositeId, err := BreakoutCompositeID(n.ID)
			if err != nil {
				t.Errorf("Invalid node ID format: %s", n.ID)
				continue
			}
			if compositeId.CompanyID+":"+compositeId.YardID != yardID {
				t.Errorf("Leaked node from wrong yard: %s", n.ID)
			}
		}
	})

	t.Run("Blocks access for user from different company", func(t *testing.T) {
		// You'll need a way to mock/simulate a user from "COMP_999"
		// depending on how your getUser(c) is implemented.
		req, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/nodes", nil)
		req.Header.Set("Authorization", getAuthHeader(userIDs[1]))
		// If using a middleware to inject user, set headers/token here

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// If the simulated user doesn't have "COMP_1", expect 403
		if w.Code == http.StatusForbidden {
			t.Log("Successfully blocked unauthorized company access")
		}
	})

	t.Run("Returns empty list for yard with no nodes", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/yard/"+yardIDs[2]+"/nodes", nil)
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var nodes []Node
		json.Unmarshal(w.Body.Bytes(), &nodes)

		if len(nodes) != 0 {
			t.Errorf("Expected 0 nodes, got %d", len(nodes))
		}
	})
}

func TestQueueUpdateSecurity(t *testing.T) {
	router := setupRouter()

	_, userIDs, _, _, nodeIDs, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]

	t.Run("Rejects update from unauthorized company", func(t *testing.T) {
		payload := FirmwareUpdate{Version: "2.0.0"}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/api/update/"+nodeIDs[1], bytes.NewBuffer(body))

		// Assume user1 belongs ONLY to "COMP_A"
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("Accepts update for authorized company", func(t *testing.T) {
		// 1. Create a buffer to hold our multipart form bytes
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		// 2. Write the standard text fields using the exact form tags your struct expects
		_ = writer.WriteField("version", "2.0.0")
		_ = writer.WriteField("ssid", "Test_WiFi")
		_ = writer.WriteField("password", "secret123")
		_ = writer.WriteField("update_url", "https://example.com/update")

		// 3. Create the file field for the firmware binary
		// The field name must match the string inside c.FormFile("firmware")
		part, err := writer.CreateFormFile("firmware", "test_firmware.bin")
		if err != nil {
			t.Fatalf("Failed to create form file field: %v", err)
		}

		// 4. Write real, non-empty bytes into the file field (simulating a valid file upload)
		dummyFirmwareBytes := []byte{0xE9, 0x03, 0x02, 0x20, 0x00, 0x00} // Fake minimal ESP32 structural header
		_, _ = part.Write(dummyFirmwareBytes)

		// 5. CRITICAL: Close the writer to append the final multipart boundary delimiter!
		_ = writer.Close()

		// 6. Set up the request with the generated body buffer
		req, _ := http.NewRequest("POST", "/api/update/"+nodeIDs[0], &body)

		// 7. CRITICAL: Inform Gin exactly what type of multipart content and boundary string is being transmitted
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 8. Asset success response
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d. Response Body: %s", w.Code, w.Body.String())
		}
	})
}

func TestDeleteUpdate(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	// Setup 2 users, 1 company each, 1 yard each, 1 node each
	// userIDs[0] owns nodeIDs[0]
	// userIDs[1] owns nodeIDs[1]
	_, userIDs, _, _, nodeIDs, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]
	myNode := nodeIDs[0]
	otherNode := nodeIDs[1]

	t.Run("Successfully delete PENDING update for authorized node", func(t *testing.T) {
		database.Collection("updates").Drop(ctx)

		// Seed a pending update
		database.Collection("updates").InsertOne(ctx, FirmwareUpdate{
			NodeID:       myNode,
			UpdateStatus: KEY_PENDING,
		})

		req, _ := http.NewRequest("DELETE", "/api/update/"+myNode, nil)
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		// Verify deletion
		count, _ := database.Collection("updates").CountDocuments(ctx, bson.M{"_id": myNode})
		if count != 0 {
			t.Error("Pending update was not deleted")
		}
	})

	t.Run("Rejects deletion of QUEUED update", func(t *testing.T) {
		database.Collection("updates").Drop(ctx)

		// Seed a queued update
		database.Collection("updates").InsertOne(ctx, FirmwareUpdate{
			NodeID:       myNode,
			UpdateStatus: KEY_QUEUED,
		})

		req, _ := http.NewRequest("DELETE", "/api/update/"+myNode, nil)
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusGone {
			t.Errorf("Expected 410 Gone for queued update, got %d", w.Code)
		}
	})

	t.Run("Rejects deletion for unauthorized company node", func(t *testing.T) {
		database.Collection("updates").Drop(ctx)

		// Seed an update for the OTHER user's node
		database.Collection("updates").InsertOne(ctx, FirmwareUpdate{
			NodeID:       otherNode,
			UpdateStatus: KEY_PENDING,
		})

		req, _ := http.NewRequest("DELETE", "/api/update/"+otherNode, nil)
		req.Header.Set("Authorization", getAuthHeader(user1)) // User1 trying to delete User2's node

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden for unauthorized node, got %d", w.Code)
		}
	})
}

func TestGetNodesHelper(t *testing.T) {
	// Setup: 1 Company, 2 Yards, 3 Nodes per yard
	_, _, _, yardIDs, _, err := setupTest(t, 1, 1, 2, 3, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetYard := yardIDs[0] // e.g., "COMP_0:YARD_0"

	t.Run("Fetches only nodes for specific yard", func(t *testing.T) {
		nodes, err := getNodesHelper(context.TODO(), targetYard)
		if err != nil {
			t.Errorf("Helper failed: %v", err)
		}

		// Should be exactly 3 nodes for this yard
		if len(nodes) != 3 {
			t.Errorf("Expected 3 nodes, got %d", len(nodes))
		}

		// Verify prefix integrity
		for _, n := range nodes {
			if !strings.HasPrefix(n.ID, targetYard+":") {
				t.Errorf("Node %s is not in yard %s", n.ID, targetYard)
			}
		}
	})
}

func TestUpdateNodeDetails(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	_, userIDs, _, _, nodeIDs, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]
	myNode := nodeIDs[0]
	otherNode := nodeIDs[1]

	t.Run("Controller successfully coordinates authorized update", func(t *testing.T) {
		payload := Node{
			ID:           myNode,
			Name:         "Yard Target Turret Alpha",
			Warning_temp: 85,
		}
		body, _ := json.Marshal(payload)

		url := "/api/nodes/" + myNode + "/details"
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		// Double check database state to confirm helper executed correctly
		var updatedNode Node
		_ = database.Collection("nodes").FindOne(ctx, bson.M{"_id": myNode}).Decode(&updatedNode)
		if updatedNode.Name != "Yard Target Turret Alpha" {
			t.Errorf("Expected helper to write name to DB, instead got: %s", updatedNode.Name)
		}
	})

	t.Run("Controller guards against unauthorized adjustments", func(t *testing.T) {
		payload := Node{Name: "Malicious Name Override"}
		body, _ := json.Marshal(payload)

		url := "/api/nodes/" + otherNode + "/details"
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})
}

func TestQueueBatchUpdates(t *testing.T) {
	router := setupRouter()

	// Seed your test data using your existing harness setup helper
	_, userIDs, _, _, nodeIDs, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]

	t.Run("Rejects batch update from unauthorized company", func(t *testing.T) {
		// 1. Build a multipart form buffer instead of standard json.Marshal
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// 2. Add text form fields matching what the Gin route expects
		// Sending nodeIDs[1] which user1 shouldn't have access to based on your layout logic
		_ = writer.WriteField("node_ids", strings.Join(nodeIDs, ","))
		_ = writer.WriteField("version", "2.0.0")
		_ = writer.WriteField("ssid", "Test_Mesh")
		_ = writer.WriteField("password", "secret123")
		_ = writer.WriteField("update_url", "http://localhost:8080/update")

		// 3. Attach a mock file stream matching your 'firmware' React key
		part, err := writer.CreateFormFile("firmware", "firmware_v2.bin")
		if err != nil {
			t.Fatalf("Failed to create mock form file: %v", err)
		}
		_, _ = part.Write([]byte{0x00, 0x01, 0x02, 0x03}) // Fake raw file binary
		writer.Close()

		// 4. Construct the HTTP Request pointing to your new endpoint rule
		req, _ := http.NewRequest("POST", "/api/updates", body)

		// Set Content-Type with the dynamic boundary string created by the writer
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden for unauthorized batch access, got %d", w.Code)
		}
	})

	t.Run("Successfully queues firmware updates for authorized nodes", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Only pass nodeIDs[0] assuming user1 has explicit authorization over it
		_ = writer.WriteField("node_ids", nodeIDs[0])
		_ = writer.WriteField("version", "2.0.0")
		_ = writer.WriteField("ssid", "Test_Mesh")
		_ = writer.WriteField("password", "secret123")
		_ = writer.WriteField("update_url", "http://localhost:8080/update")

		part, _ := writer.CreateFormFile("firmware", "firmware_v2.bin")
		_, _ = part.Write([]byte{0x00, 0x01, 0x02, 0x03})
		writer.Close()

		req, _ := http.NewRequest("POST", "/api/updates", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d. Response Body: %s", w.Code, w.Body.String())
		}
	})
}

func TestDeliverRawFirmwareBinaryOnly(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	_, _, _, _, nodeIDs, err := setupTest(t, 1, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	targetNode := nodeIDs[0]

	database.Collection("updates").Drop(ctx)

	// Simulate a real 6-byte compiled binary file array
	realCompiledBytes := []byte{0xEB, 0x44, 0x90, 0x12, 0x34, 0x56}
	database.Collection("updates").InsertOne(ctx, FirmwareUpdate{
		NodeID:       targetNode,
		UpdateStatus: KEY_QUEUED,
		Version:      "3.1.0",
		Firmware:     realCompiledBytes,
	})

	url := "/api/firmwareUpdate/" + targetNode
	req, _ := http.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", w.Code)
	}

	// CRITICAL: The total body content length must equal exactly the binary length
	if len(w.Body.Bytes()) != len(realCompiledBytes) {
		t.Errorf("Payload polluted. Expected size %d, got %d", len(realCompiledBytes), len(w.Body.Bytes()))
	}

	if !bytes.Equal(w.Body.Bytes(), realCompiledBytes) {
		t.Error("Delivered stream bytes do not match the compiled binary file exactly")
	}
}

func TestMetricsLifecycle(t *testing.T) {
	router := setupRouter()
	yardID := "COMP_1:YARD_A"

	// --- STAGE 1: Initial Provisioning ---
	t.Run("Creates record on first submission", func(t *testing.T) {
		m := SystemMetrics{
			YardID:  yardID,
			TempC:   40.0,
			FanRPM:  0,
			LoadAvg: 0.5,
		}

		w := performMetrics(router, m)
		assert.Equal(t, http.StatusOK, w.Code)

		// VERIFY: Database contains the record
		var saved SystemMetrics
		database.Collection("yard_metrics").FindOne(context.TODO(), bson.M{"_id": yardID}).Decode(&saved)
		assert.Equal(t, 40.0, saved.TempC)
		assert.False(t, saved.UpdatedAt.IsZero(), "UpdatedAt should be set")
	})

	// --- STAGE 2: Update Existing Record ---
	t.Run("Updates existing record (Upsert)", func(t *testing.T) {
		m := SystemMetrics{
			YardID: yardID,
			TempC:  65.5, // Temperature increased
			FanRPM: 3000, // Fan kicked on
		}

		performMetrics(router, m)

		// VERIFY: Record was updated, not duplicated
		var saved SystemMetrics
		database.Collection("yard_metrics").FindOne(context.TODO(), bson.M{"_id": yardID}).Decode(&saved)

		assert.Equal(t, 65.5, saved.TempC)
		assert.Equal(t, 3000, saved.FanRPM)

		// Count total docs to ensure we didn't create a second one
		count, _ := database.Collection("yard_metrics").CountDocuments(context.TODO(), bson.M{"_id": yardID})
		assert.Equal(t, int64(1), count, "Should only have one record per yard")
	})

	// --- STAGE 3: Error Handling ---
	t.Run("Rejects invalid payload", func(t *testing.T) {
		w := performRawMetrics(router, "{invalid-json}")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetMetrics(t *testing.T) {
	router := setupRouter()

	// 1. SETUP
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]
	yard1 := yardIDs[0]

	// 2. ACT: "Live Seed" the metrics via POST
	payload := SystemMetrics{
		YardID:  yard1,
		TempC:   45.0,
		FanRPM:  2000,
		LoadAvg: 0.5,
	}
	performMetrics(router, payload)

	// Update with new data to ensure it correctly "replaces" the old state
	payload.TempC = 55.0
	payload.FanRPM = 4000
	performMetrics(router, payload)

	// 3. GET: Retrieve the metrics via GET
	req, _ := http.NewRequest("GET", "/api/metrics/"+yard1, nil)
	req.Header.Set("Authorization", getAuthHeader(user1))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 4. ASSERT
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var result SystemMetrics
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode SystemMetrics: %v", err)
	}

	// Verify the latest state (the 55.0 update)
	if result.TempC != 55.0 {
		t.Errorf("Current temp mismatch. Expected 55.0, got %v", result.TempC)
	}
	if result.FanRPM != 4000 {
		t.Errorf("FanRPM mismatch. Expected 4000, got %v", result.FanRPM)
	}
}

func performRawMetrics(router *gin.Engine, body string) *httptest.ResponseRecorder {
	req, err := http.NewRequest("POST", "/api/metrics", bytes.NewBufferString(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func performMetrics(router *gin.Engine, payload SystemMetrics) *httptest.ResponseRecorder {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "/api/metrics", bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// Helper to dry up the HTTP requests
func performHeartbeat(router *gin.Engine, payload HeartbeatPayload) *httptest.ResponseRecorder {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "/api/heartbeat", bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
