package main

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// getCompaniesHelper returns all companies assigned to a specific user's allowed list
func getCompaniesHelper(ctx context.Context, user User) ([]Company, error) {
	if len(user.CompanyIDs) == 0 {
		return []Company{}, nil
	}

	query := bson.M{"_id": bson.M{"$in": user.CompanyIDs}}
	cursor, err := database.Collection("companies").Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var companies []Company
	if err := cursor.All(ctx, &companies); err != nil {
		return nil, err
	}
	return companies, nil
}

// getAllCompanySummaries returns summaries for all companies (admin only)
func getAllCompanySummaries(ctx context.Context) ([]CompanySummary, error) {
	// 1. Get ALL company objects (no filter)
	cursor, err := database.Collection("companies").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var companies []Company
	if err = cursor.All(ctx, &companies); err != nil {
		return nil, err
	}

	var summaries []CompanySummary

	// 2. Reuse your existing aggregation logic
	for _, comp := range companies {
		yards, err := getYardSummaries(ctx, comp.ID)
		if err != nil {
			return nil, err
		}

		nodeCount := 0
		badStatus := false

		for _, yard := range yards {
			nodeCount += yard.NodeCount
			if yard.UnhealthyCount > 0 {
				badStatus = true
			}
		}

		summaries = append(summaries, CompanySummary{
			ID:           comp.ID,
			YardCount:    len(yards),
			NodeCount:    nodeCount,
			HasBadStatus: badStatus,
		})
	}

	return summaries, nil
}
