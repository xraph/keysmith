// Command forge demonstrates Keysmith as a Forge extension.
package main

import (
	"fmt"

	"github.com/xraph/keysmith/extension"
	"github.com/xraph/keysmith/store/memory"

	"github.com/xraph/keysmith"
)

func main() {
	// Build the Keysmith Forge extension with an in-memory store.
	ext := extension.New(
		extension.WithConfig(extension.Config{
			DisableMigrate: true, // Memory store doesn't need migrations.
		}),
		extension.WithEngineOptions(
			keysmith.WithStore(memory.New()),
		),
	)

	fmt.Println("Extension Name:", ext.Name())
	fmt.Println("Extension Version:", ext.Version())
	fmt.Println("Extension Description:", ext.Description())

	// In a real Forge app, you would register this extension:
	//
	//   app := forge.New(
	//       forge.WithExtension(ext),
	//   )
	//   app.Run()
	//
	// The extension will:
	// 1. Create the Keysmith engine during Register
	// 2. Register the engine in the DI container (vessel)
	// 3. Mount REST API routes on the Forge router
	// 4. Run migrations on Start (unless disabled)
	// 5. Gracefully shut down on Stop
}
