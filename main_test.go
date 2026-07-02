package main

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file - make sure it exists!")
	}
	// 1. Set environment
	testSecret := "test_secret_for_livermore_logic"
	os.Setenv("AUTH_JWT_SECRET", testSecret)

	// 2. Initialize with a SAFE test name
	testDBName := "TEST_ONLY_remote_server"
	initDB(testDBName)
	initAuth()

	// 3. Run tests
	code := m.Run()

	// 4. CLEANUP: Drop the entire test database so it disappears from Mongo
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// We double-check the name contains "TEST" just in case
	if strings.Contains(database.Name(), "TEST") {
		database.Drop(ctx)
	}

	os.Exit(code)
}

func setupTestDB(t *testing.T) *mongo.Database {
	// Use in-memory MongoDB for testing if available, otherwise skip
	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		t.Skip("MongoDB not available for testing")
	}

	// Create test database unique per test
	testDB := client.Database("test_" + t.Name())

	// Clean up any existing data
	collections := []string{"nodes", "history", "updates", "users", "yards"}
	for _, coll := range collections {
		testDB.Collection(coll).Drop(ctx)
	}

	// Set global database for tests
	database = testDB

	return testDB
}

func TestSetupTest_PerUserScaling(t *testing.T) {
	// 2 Users * 2 Companies * 3 Yards * 4 Nodes
	u, c, y, n := 2, 2, 3, 4

	_, userIDs, companyIDs, yardIDs, nodeIDs, err := setupTest(t, u, c, y, n, time.Now())
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	ctx := context.TODO()

	// 1. Check Companies: 2 Users * 2 per user = 4 total
	expectedCompanies := u * c
	if len(companyIDs) != expectedCompanies {
		t.Errorf("Expected %d total companies, got %d", expectedCompanies, len(companyIDs))
	}

	// 2. Check Yards: 4 companies * 3 yards = 12 total
	expectedYards := expectedCompanies * y
	if len(yardIDs) != expectedYards {
		t.Errorf("Expected %d total yards, got %d", expectedYards, len(yardIDs))
	}

	// 3. Check Nodes: 12 yards * 4 nodes = 48 total
	expectedNodes := expectedYards * n
	if len(nodeIDs) != expectedNodes {
		t.Errorf("Expected %d nodes in slice, got %d", expectedNodes, len(nodeIDs))
	}

	countNodes, _ := database.Collection("nodes").CountDocuments(ctx, bson.M{})
	if int(countNodes) != expectedNodes {
		t.Errorf("Expected %d total nodes in DB, got %d", expectedNodes, countNodes)
	}

	// 4. Check that User 1 doesn't own User 0's companies
	var user1 User
	database.Collection("users").FindOne(ctx, bson.M{"_id": userIDs[1]}).Decode(&user1)

	if len(user1.CompanyIDs) != c {
		t.Errorf("User 1 should only have %d companies, found %d", c, len(user1.CompanyIDs))
	}
}
