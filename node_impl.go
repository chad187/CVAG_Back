package main

import (
	"context"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// getNodesHelper fetches all nodes belonging to a specific yard using prefix matching
func getNodesHelper(ctx context.Context, yardID string) ([]Node, error) {
	// Using your COMP_0:YARD_0: prefix convention
	prefix := yardID + ":"
	query := bson.M{"_id": bson.M{"$regex": "^" + prefix}}

	cursor, err := database.Collection("nodes").Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var nodes []Node
	if err := cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}

	ApplyLivenessStatus(ctx, &nodes)

	return nodes, nil
}

// GetNodesByYardID fetches all nodes for a specific yard
func GetNodesByYardID(ctx context.Context, yardID string) ([]Node, error) {
	pattern := "^" + regexp.QuoteMeta(yardID) + ":"

	cursor, err := database.Collection("nodes").Find(ctx, bson.M{
		"_id": bson.M{"$regex": pattern},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var nodes []Node
	if err = cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}

	ApplyLivenessStatus(ctx, &nodes)

	return nodes, nil
}

// CreateFirmwareUpdate creates and queues a firmware update for a node
func CreateFirmwareUpdate(ctx context.Context, nodeID string, update FirmwareUpdate) (FirmwareUpdate, error) {
	if len(update.Firmware) == 0 || update.Version == "" || update.SSID == "" || update.Password == "" {
		return FirmwareUpdate{}, errors.New("all firmware update fields are required")
	}
	// 1. Fetch current node
	var node Node
	if err := database.Collection("nodes").FindOne(ctx, bson.M{"_id": nodeID}).Decode(&node); err != nil {
		return FirmwareUpdate{}, err
	}

	// 2. Build update with scheduling
	interval := GetPredictedInterval(ctx, node)
	update.NodeID = nodeID
	update.UpdateStatus = KEY_PENDING
	update.CreatedAt = time.Now()
	update.UpdateAt = node.UpdatedAt.Add(interval)

	// 3. Save to database
	if err := UpsertFirmwareUpdate(ctx, update); err != nil {
		return FirmwareUpdate{}, err
	}

	return update, nil
}

// CancelPendingUpdate attempts to cancel a pending or queued firmware update
func CancelPendingUpdate(ctx context.Context, nodeID string) (CancelUpdateResult, error) {
	// 1. Try to delete pending update
	result, err := database.Collection("updates").DeleteOne(ctx, bson.M{
		"_id":           nodeID,
		"update_status": KEY_PENDING,
	})

	if err != nil {
		return CancelUpdateResult{}, err
	}

	// 2. If delete succeeded, we're done
	if result.DeletedCount > 0 {
		return CancelUpdateResult{Cancelled: true}, nil
	}

	// 3. Check if update exists in another state
	result, err = database.Collection("updates").DeleteOne(ctx, bson.M{
		"_id":           nodeID,
		"update_status": KEY_QUEUED,
	})

	if err != nil {
		return CancelUpdateResult{}, err
	}

	// 4. Update exists but not in pending state
	if result.DeletedCount > 0 {
		return CancelUpdateResult{Cancelled: true, AlreadyQueued: true}, nil
	}

	// 5. Update exists in some other state (shouldn't happen normally)
	return CancelUpdateResult{NotFound: true}, nil
}

// ProcessHeartbeat handles the full heartbeat processing logic
func ProcessHeartbeat(ctx context.Context, payload HeartbeatPayload, compositeID CompositeID) error {
	// 1. Auto-Provisioning (Ensures breadcrumbs exist for the dashboard)
	ensureHierarchyExists(ctx, compositeID)

	// 2. Archiving (Moving current status to history before updating)
	var oldStatus Node
	if err := database.Collection("nodes").FindOne(ctx, bson.M{"_id": payload.Current.ID}).Decode(&oldStatus); err == nil {
		database.Collection("history").InsertOne(ctx, HistoryRecord{
			NodeID:    oldStatus.ID,
			Temp:      oldStatus.Temp,
			Timestamp: oldStatus.UpdatedAt,
		})
	}

	// 3. Process Batch History (Buffered data from the local server)
	if len(payload.History) > 0 {
		var batch []interface{}
		for _, item := range payload.History {
			batch = append(batch, HistoryRecord{
				NodeID:    item.ID,
				Temp:      item.Temp,
				Timestamp: item.UpdatedAt,
			})
		}
		database.Collection("history").InsertMany(ctx, batch)
	}

	// 4. Update Current Status
	// Define the pipeline
	pipeline := mongo.Pipeline{
		// STAGE 1: Update raw values and ensure warning_temp exists
		{{Key: "$set", Value: bson.D{
			{Key: "temp", Value: payload.Current.Temp},
			{Key: "battery", Value: payload.Current.Battery},
			{Key: "version", Value: payload.Current.Version},
			{Key: "updated_at", Value: payload.Current.UpdatedAt},
			{Key: "name", Value: bson.D{
				{Key: "$ifNull", Value: bson.A{"$name", payload.Current.ID}},
			}},
			{Key: "warning_temp", Value: bson.D{
				{Key: "$ifNull", Value: bson.A{"$warning_temp", KEY_WARNING_TEMP}},
			}},
		}}},

		// STAGE 2: Calculate status based on the values written in Stage 1
		{{Key: "$set", Value: bson.D{
			{Key: "status", Value: bson.D{
				{Key: "$cond", Value: bson.D{
					// 1. Check Heat
					{Key: "if", Value: bson.D{
						{Key: "$gt", Value: bson.A{"$temp", "$warning_temp"}},
					}},
					{Key: "then", Value: KEY_OVERHEATING},

					// 2. Else Check Battery
					{Key: "else", Value: bson.D{
						{Key: "$cond", Value: bson.D{
							{Key: "if", Value: bson.D{
								{Key: "$lt", Value: bson.A{"$battery", KEY_LOW_BATTERY}},
							}},
							{Key: "then", Value: KEY_LOW_BATTERY_STATUS},

							// 3. Fallback to OK
							{Key: "else", Value: KEY_OK},
						}},
					}},
				}},
			}},
		}}},
	}

	// 5. Update node with the pipeline
	opts := options.UpdateOne().SetUpsert(true)
	_, err := database.Collection("nodes").UpdateOne(ctx, bson.M{"_id": payload.Current.ID}, pipeline, opts)

	return err
}

// EditNodeDetails handles the business logic and DB payload execution for modifying a node.
func EditNodeDetails(ctx context.Context, nodeID string, input Node) error {
	// Perform any business logic or validation rules here
	if nodeID == "" {
		return errors.New("node ID cannot be empty")
	}

	if input.Warning_temp <= 0 {
		return errors.New("warning_temp must be a positive number")
	}

	if input.Name == "" {
		input.Name = nodeID // Default to ID if name is empty
	}

	filter := bson.M{"_id": nodeID}
	update := bson.M{
		"$set": bson.M{
			"name":         input.Name,
			"warning_temp": input.Warning_temp,
			"updated_at":   time.Now(),
		},
	}

	result, err := database.Collection("nodes").UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments // Standard way to bubble up a 404 condition
	}

	return nil
}

// GetQueuedFirmware retrieves the firmware payload for a node without modifying its state.
func GetQueuedFirmware(ctx context.Context, nodeID string) (*FirmwareUpdate, error) {
	if nodeID == "" {
		return nil, errors.New("node ID cannot be empty")
	}

	filter := bson.M{
		"_id":           nodeID,
		"update_status": KEY_QUEUED, // Ensure we are still only serving updates slated for delivery
	}

	var firmwareRecord FirmwareUpdate
	err := database.Collection("updates").FindOne(ctx, filter).Decode(&firmwareRecord)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, mongo.ErrNoDocuments
		}
		return nil, err
	}

	return &firmwareRecord, nil
}

func getAllNodeSummaries(ctx context.Context) ([]Node, error) {
	cursor, err := database.Collection("nodes").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var nodes []Node
	if err = cursor.All(ctx, &nodes); err != nil {
		return nil, err
	}

	ApplyLivenessStatus(ctx, &nodes)

	if nodes == nil {
		nodes = []Node{}
	}
	return nodes, nil
}

// ApplyLivenessStatus performs both in-memory updates and the DB save
func ApplyLivenessStatus(ctx context.Context, nodes *[]Node) {
	threshold := time.Now().Add(-24 * time.Hour)
	var staleIDs []string

	for i := range *nodes {
		node := &(*nodes)[i]
		if node.Status != KEY_OFFLINE && node.UpdatedAt.Before(threshold) {
			node.Status = KEY_OFFLINE
			staleIDs = append(staleIDs, node.ID)
		}
	}

	if len(staleIDs) > 0 {
		go func(ids []string) {
			// Directly access your global/package-level database variable
			filter := bson.M{"_id": bson.M{"$in": ids}}
			update := bson.M{"$set": bson.M{"status": KEY_OFFLINE}}

			// Assuming 'database' is your package-level DB handle
			database.Collection("nodes").UpdateMany(context.Background(), filter, update)
		}(staleIDs)
	}
}
