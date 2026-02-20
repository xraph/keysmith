"use client";

import { motion } from "framer-motion";
import { cn } from "@/lib/cn";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

interface FeatureCard {
  title: string;
  description: string;
  icon: React.ReactNode;
  code: string;
  filename: string;
  colSpan?: number;
}

const features: FeatureCard[] = [
  {
    title: "API Key Lifecycle",
    description:
      "Create, validate, rotate, revoke, suspend, and reactivate API keys. Raw keys are returned once at creation; only SHA-256 hashes are stored.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 11-7.778 7.778 5.5 5.5 0 017.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
      </svg>
    ),
    code: `result, _ := eng.CreateKey(ctx,
  &keysmith.CreateKeyInput{
    Name:   "Production Key",
    Prefix: "sk",
    Environment: key.EnvLive,
    Scopes: []string{"read:users"},
  })

fmt.Println(result.RawKey)
// sk_live_a3f8b2c9e1d4...`,
    filename: "create.go",
  },
  {
    title: "Scoped Permissions",
    description:
      "Assign hierarchical permission scopes to keys. Validate that a key has the required scopes before granting access to protected resources.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
    ),
    code: `// Assign scopes to a key
eng.AssignScopes(ctx, keyID,
  []string{"read:users", "write:users"})

// Validate key and check scopes
vr, _ := eng.ValidateKey(ctx, rawKey)
fmt.Println(vr.Scopes)
// [read:users write:users]`,
    filename: "scopes.go",
  },
  {
    title: "Policy Engine",
    description:
      "Rate limits, IP allowlists, origin restrictions, key lifetime constraints, and usage quotas. Attach policies to keys for fine-grained control.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="3" />
        <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" />
      </svg>
    ),
    code: `eng.CreatePolicy(ctx, &policy.Policy{
  Name:         "Standard",
  RateLimit:    1000,
  RateWindow:   time.Minute,
  AllowedIPs:   []string{"10.0.0.0/8"},
  MaxKeyAge:    90 * 24 * time.Hour,
})`,
    filename: "policies.go",
  },
  {
    title: "Key Rotation",
    description:
      "Zero-downtime key rotation with configurable grace periods. Old keys remain valid during the grace window while clients migrate to the new key.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M1 4v6h6M23 20v-6h-6" />
        <path d="M20.49 9A9 9 0 005.64 5.64L1 10M23 14l-4.64 4.36A9 9 0 013.51 15" />
      </svg>
    ),
    code: `newResult, _ := eng.RotateKey(ctx,
  keyID,
  rotation.ReasonScheduled,
  24*time.Hour, // grace period
)

// Old key still valid for 24 hours
// New key: newResult.RawKey`,
    filename: "rotation.go",
  },
  {
    title: "Multi-Tenant Isolation",
    description:
      "Every operation is scoped to app and tenant via context. Cross-tenant access is structurally impossible at the store layer.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
        <circle cx="9" cy="7" r="4" />
        <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
      </svg>
    ),
    code: `ctx = keysmith.WithTenant(ctx,
  "my-app", "tenant-1")

// All key operations are automatically
// scoped to this app + tenant.
// Keys, policies, scopes, and usage
// are fully isolated per tenant.`,
    filename: "scope.go",
  },
  {
    title: "Plugin System",
    description:
      "Opt-in lifecycle hooks for audit trails, metrics, and authorization sync. Register plugins that fire on key creation, validation, rotation, and more.",
    icon: (
      <svg
        className="size-5"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 2L2 7l10 5 10-5-10-5z" />
        <path d="M2 17l10 5 10-5M2 12l10 5 10-5" />
      </svg>
    ),
    code: `eng, _ := keysmith.NewEngine(
  keysmith.WithStore(store),
  keysmith.WithExtension(
    audithook.New(myRecorder)),
  keysmith.WithExtension(
    observability.NewMetricsExtension()),
)`,
    filename: "plugins.go",
    colSpan: 2,
  },
];

const containerVariants = {
  hidden: {},
  visible: {
    transition: {
      staggerChildren: 0.08,
    },
  },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease: "easeOut" as const },
  },
};

export function FeatureBento() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Features"
          title="Everything you need for API key management"
          description="Keysmith handles the hard parts — key generation, hash storage, tenant isolation, policy enforcement, and usage analytics — so you can focus on your application."
        />

        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-50px" }}
          className="mt-14 grid grid-cols-1 md:grid-cols-2 gap-4"
        >
          {features.map((feature) => (
            <motion.div
              key={feature.title}
              variants={itemVariants}
              className={cn(
                "group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm p-6 hover:border-amber-500/20 hover:bg-fd-card/80 transition-all duration-300",
                feature.colSpan === 2 && "md:col-span-2",
              )}
            >
              {/* Header */}
              <div className="flex items-start gap-3 mb-4">
                <div className="flex items-center justify-center size-9 rounded-lg bg-amber-500/10 text-amber-600 dark:text-amber-400 shrink-0">
                  {feature.icon}
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-fd-foreground">
                    {feature.title}
                  </h3>
                  <p className="text-xs text-fd-muted-foreground mt-1 leading-relaxed">
                    {feature.description}
                  </p>
                </div>
              </div>

              {/* Code snippet */}
              <CodeBlock
                code={feature.code}
                filename={feature.filename}
                showLineNumbers={false}
                className="text-xs"
              />
            </motion.div>
          ))}
        </motion.div>
      </div>
    </section>
  );
}
