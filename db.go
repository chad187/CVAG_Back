package main

import (
	"context"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var client *mongo.Client
var database *mongo.Database

func initDB(dbName string) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017" // Fallback for local dev
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	client, err = mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal(err)
	}

	database = client.Database(dbName)

	// --- 1. HISTORY COLLECTION ---
	// TTL Index: Purge history older than 365 days
	ttlIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "timestamp", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(31536000),
	}
	database.Collection("history").Indexes().CreateOne(ctx, ttlIndex)

	// Compound Index for Time-Series: (Full Node ID + Timestamp)
	// This makes "Get last 50 readings for node X" extremely fast.
	historyIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "node_id", Value: 1},    // company:yard:node
			{Key: "timestamp", Value: -1}, // Newest data first
		},
	}
	_, err = database.Collection("history").Indexes().CreateOne(ctx, historyIndex)
	if err != nil {
		log.Printf("Error creating history compound index: %v", err)
	}

	// --- 2. NODES COLLECTION ---
	// Lookup by Yard ID (company:yard)
	nodeIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "yard_id", Value: 1}},
	}
	database.Collection("nodes").Indexes().CreateOne(ctx, nodeIndex)

	// --- 3. YARDS COLLECTION ---
	// Lookup yards by Owner IDs (Multikey index for the string array)
	yardIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "owner_ids", Value: 1}},
	}
	database.Collection("yards").Indexes().CreateOne(ctx, yardIndex)

	// --- 4. UPDATES COLLECTION ---
	// Optimize the heartbeat check: find "pending" for a specific node
	updateIndex := mongo.IndexModel{
		Keys: bson.D{
			{Key: "node_id", Value: 1},
			{Key: "update_status", Value: 1},
		},
	}
	database.Collection("updates").Indexes().CreateOne(ctx, updateIndex)

	// --- 5. COMPANIES COLLECTION ---
	// Lookup companies by Owner IDs
	companyIndex := mongo.IndexModel{
		Keys: bson.D{{Key: "owner_ids", Value: 1}},
	}
	database.Collection("companies").Indexes().CreateOne(ctx, companyIndex)

	// --- 6. USERS COLLECTION ---
	// Unique index for social login
	userIndex := mongo.IndexModel{
		Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "provider_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	_, err = database.Collection("users").Indexes().CreateOne(ctx, userIndex)
	if err != nil {
		log.Printf("Error creating user index: %v", err)
	}
}
