package main

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// getYardsHelper fetches all yards belonging to a specific company ID using prefix matching
func getYardsHelper(ctx context.Context, companyID string) ([]Yard, error) {
	// Using your COMP_0: prefix convention
	prefix := companyID + ":"
	query := bson.M{"_id": bson.M{"$regex": "^" + prefix}}

	cursor, err := database.Collection("yards").Find(ctx, query)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var yards []Yard
	if err := cursor.All(ctx, &yards); err != nil {
		return nil, err
	}
	return yards, nil
}

func getYardSummaries(ctx context.Context, companyID string) ([]YardSummary, error) {
	// 1. Get the yards for this company
	yards, err := getYardsHelper(ctx, companyID)
	if err != nil {
		return nil, err
	}

	var summaries []YardSummary

	for _, y := range yards {
		nodes, err := getNodesHelper(ctx, y.ID)
		if err != nil {
			return nil, err
		}

		unhealthyCount := 0
		for _, node := range nodes {
			if node.Status != KEY_OK {
				unhealthyCount++
			}
		}

		summaries = append(summaries, YardSummary{
			ID:             y.ID,
			NodeCount:      len(nodes),
			UnhealthyCount: unhealthyCount,
		})
	}

	return summaries, nil
}

func getAllYardSummaries(ctx context.Context) (summary []YardSummary, err error) {
	cursor, err := database.Collection("companies").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var companies []Company
	if err = cursor.All(ctx, &companies); err != nil {
		return nil, err
	}

	for _, comp := range companies {
		yards, err2 := getYardSummaries(ctx, comp.ID)
		if err2 != nil {
			return
		}
		summary = append(summary, yards...)
	}

	return
}
