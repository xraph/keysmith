"use client";

import { motion } from "framer-motion";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

const createKeyCode = `package main

import (
  "context"
  "fmt"
  "log"

  "github.com/xraph/keysmith"
  "github.com/xraph/keysmith/key"
  "github.com/xraph/keysmith/store/memory"
)

func main() {
  ctx := context.Background()
  eng, _ := keysmith.NewEngine(
    keysmith.WithStore(memory.New()),
  )

  ctx = keysmith.WithTenant(ctx,
    "my-app", "tenant-1")

  // Create an API key — raw key shown once
  result, _ := eng.CreateKey(ctx,
    &keysmith.CreateKeyInput{
      Name:   "Production Key",
      Prefix: "sk",
      Environment: key.EnvLive,
      Scopes: []string{"read:users"},
    })

  fmt.Println("Key:", result.RawKey)
  // sk_live_a3f8b2c9e1d4...
}`;

const validateKeyCode = `package main

import (
  "context"
  "fmt"
  "log"

  "github.com/xraph/keysmith"
  "github.com/xraph/keysmith/store/memory"
)

func main() {
  ctx := context.Background()
  eng, _ := keysmith.NewEngine(
    keysmith.WithStore(memory.New()),
  )

  ctx = keysmith.WithTenant(ctx,
    "my-app", "tenant-1")

  // Validate an incoming API key
  vr, err := eng.ValidateKey(ctx, rawKey)
  if err != nil {
    log.Fatal("invalid key:", err)
  }

  fmt.Println("Tenant:", vr.Key.TenantID)
  fmt.Println("Scopes:", vr.Scopes)
  fmt.Println("State:", vr.Key.State)
  // Tenant: tenant-1
  // Scopes: [read:users]
  // State: active
}`;

export function CodeShowcase() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Developer Experience"
          title="Simple API. Powerful primitives."
          description="Create and validate API keys in under 20 lines. Keysmith handles hashing, scoping, and lifecycle management."
        />

        <div className="mt-14 grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Create key side */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-amber-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Create Key
              </span>
            </div>
            <CodeBlock code={createKeyCode} filename="main.go" />
          </motion.div>

          {/* Validate key side */}
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.2 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-orange-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Validate Key
              </span>
            </div>
            <CodeBlock code={validateKeyCode} filename="validate.go" />
          </motion.div>
        </div>
      </div>
    </section>
  );
}
