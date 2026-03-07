"use client";

import { motion } from "framer-motion";
import { CodeBlock } from "./code-block";
import { FeatureBullet } from "./feature-bullet";
import { FlowLine, FlowNode } from "./flow-primitives";
import { SectionHeader } from "./section-header";

// ─── Middleware Code Example ────────────────────────────────
const middlewareCode = `t, _ := trove.Open(drv,
    trove.WithMiddleware(
        compress.New(),
        encrypt.New(encrypt.WithKeyProvider(kp)),
        dedup.New(),
    ),
)

// Middleware runs automatically on Put/Get
t.Put(ctx, "uploads", "data.csv", reader)`;

// ─── Pipeline Visualization ─────────────────────────────────
function MiddlewarePipelineDiagram() {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      whileInView={{ opacity: 1 }}
      viewport={{ once: true }}
      transition={{ duration: 0.6 }}
      className="relative"
    >
      {/* Background glow */}
      <div className="absolute inset-0 -m-6 bg-linear-to-br from-green-500/5 via-transparent to-teal-500/5 rounded-3xl blur-xl" />

      <div className="relative p-3 sm:p-6 rounded-2xl border border-fd-border/50 bg-fd-card/30 backdrop-blur-sm">
        <div className="flex flex-col items-center gap-6">
          {/* Pipeline flow: compress → encrypt → dedup */}
          <div className="flex items-center gap-0">
            <FlowNode
              label="compress"
              color="green"
              size="sm"
              pulse
              delay={0.2}
              icon={
                <svg
                  className="size-3"
                  viewBox="0 0 12 12"
                  fill="none"
                  aria-hidden="true"
                >
                  <path
                    d="M3 2v8M6 4v4M9 3v6"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                  />
                </svg>
              }
            />
            <FlowLine length={40} color="green" delay={1} />
            <FlowNode
              label="encrypt"
              color="blue"
              size="sm"
              pulse
              delay={0.4}
              icon={
                <svg
                  className="size-3"
                  viewBox="0 0 12 12"
                  fill="none"
                  aria-hidden="true"
                >
                  <rect
                    x="2"
                    y="5"
                    width="8"
                    height="6"
                    rx="1"
                    stroke="currentColor"
                    strokeWidth="1.5"
                  />
                  <path
                    d="M4 5V3.5a2 2 0 014 0V5"
                    stroke="currentColor"
                    strokeWidth="1.5"
                  />
                </svg>
              }
            />
            <FlowLine length={40} color="blue" delay={2} />
            <FlowNode
              label="dedup"
              color="purple"
              size="sm"
              pulse
              delay={0.6}
              icon={
                <svg
                  className="size-3"
                  viewBox="0 0 12 12"
                  fill="none"
                  aria-hidden="true"
                >
                  <circle
                    cx="6"
                    cy="6"
                    r="4"
                    stroke="currentColor"
                    strokeWidth="1.5"
                  />
                  <path
                    d="M4 6l2 2 2-2"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              }
            />
          </div>

          {/* Status indicators */}
          <div className="flex items-center gap-4 text-[10px] text-fd-muted-foreground">
            <div className="flex items-center gap-1.5">
              <div className="size-2 rounded-full bg-green-500" />
              <span>Compressed</span>
            </div>
            <div className="flex items-center gap-1.5">
              <motion.div
                className="size-2 rounded-full bg-blue-400"
                animate={{ opacity: [1, 0.4, 1] }}
                transition={{ duration: 2, repeat: Infinity }}
              />
              <span>Encrypted</span>
            </div>
            <div className="flex items-center gap-1.5">
              <div className="size-2 rounded-full bg-purple-500" />
              <span>Deduplicated</span>
            </div>
          </div>

          {/* Code block */}
          <div className="w-full">
            <CodeBlock
              code={middlewareCode}
              filename="pipeline.go"
              showLineNumbers={false}
              className="text-xs"
            />
          </div>
        </div>
      </div>
    </motion.div>
  );
}

// ─── Middleware Showcase Section ─────────────────────────────
export function MiddlewareShowcase() {
  return (
    <section className="relative w-full py-20 sm:py-28 overflow-hidden">
      {/* Background */}
      <div className="absolute inset-0 bg-linear-to-b from-transparent via-green-500/2 to-transparent" />

      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <div className="grid gap-12 lg:grid-cols-2 lg:gap-16 items-center">
          {/* Left: Text content */}
          <div className="flex flex-col">
            <SectionHeader
              badge="Middleware"
              title="Composable by design."
              description="Direction-aware, scope-aware middleware pipeline. Stack compression, encryption, deduplication, virus scanning, and watermarking in any order."
              align="left"
            />

            <div className="mt-8 space-y-5">
              <FeatureBullet
                title="Compression"
                description="Zstd compression with automatic skip lists for already-compressed formats like JPEG, PNG, and ZIP."
                delay={0.2}
              />
              <FeatureBullet
                title="Encryption"
                description="AES-256-GCM with pluggable KeyProvider interface for key rotation and integration with Vault or KMS."
                delay={0.3}
              />
              <FeatureBullet
                title="Deduplication"
                description="BLAKE3 content hashing to detect and eliminate duplicate objects. Reference counting prevents premature deletion."
                delay={0.4}
              />
            </div>

            <motion.div
              initial={{ opacity: 0 }}
              whileInView={{ opacity: 1 }}
              viewport={{ once: true }}
              transition={{ delay: 0.5 }}
              className="mt-8"
            >
              <a
                href="/docs/storage/middleware"
                className="inline-flex items-center gap-1 text-sm font-medium text-blue-600 dark:text-blue-400 hover:text-blue-500 transition-colors"
              >
                Learn about middleware
                <svg
                  className="size-3.5"
                  viewBox="0 0 16 16"
                  fill="none"
                  aria-hidden="true"
                >
                  <path
                    d="M6 4l4 4-4 4"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              </a>
            </motion.div>
          </div>

          {/* Right: Pipeline visualization */}
          <div className="relative">
            <MiddlewarePipelineDiagram />
          </div>
        </div>
      </div>
    </section>
  );
}
