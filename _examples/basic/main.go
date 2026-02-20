// Command basic demonstrates standalone Keysmith usage without Forge.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store/memory"
)

func main() {
	// Create an in-memory store (replace with postgres in production).
	ms := memory.New()

	// Build the engine.
	eng, err := keysmith.NewEngine(keysmith.WithStore(ms))
	if err != nil {
		log.Fatal(err)
	}

	// Set up a tenant context (standalone mode).
	ctx := keysmith.WithTenant(context.Background(), "my-app", "tenant-1")

	// Create an API key.
	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "My First API Key",
		Prefix:      "sk",
		Environment: key.EnvLive,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Raw API Key (save this — shown once):", result.RawKey)
	fmt.Println("Key ID:", result.Key.ID)
	fmt.Println("Key Hint:", result.Key.Hint)

	// Validate the key.
	vr, err := eng.ValidateKey(ctx, result.RawKey)
	if err != nil {
		log.Fatal("validation failed:", err)
	}
	fmt.Println("Validated key for tenant:", vr.Key.TenantID)

	// Revoke the key.
	if err := eng.RevokeKey(ctx, result.Key.ID, "demo cleanup"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Key revoked successfully")

	// Validation should now fail.
	_, err = eng.ValidateKey(ctx, result.RawKey)
	fmt.Println("Post-revoke validation error:", err)
}
