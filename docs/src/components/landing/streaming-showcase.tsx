"use client";

import { motion } from "framer-motion";
import { CodeBlock } from "./code-block";
import { FeatureBullet } from "./feature-bullet";
import { FlowLine, FlowNode } from "./flow-primitives";
import { SectionHeader } from "./section-header";

// ─── Streaming Code Sample ──────────────────────────────────
const streamCode = `s, _ := t.Stream(ctx, "media", "video.mp4",
    stream.Upload,
    stream.WithChunkSize(16 * 1024 * 1024),
    stream.WithBackpressure(stream.BackpressureBlock),
)
defer s.Close()

for s.Next() {
    chunk := s.Chunk()
    // process chunk...
}`;

// ─── Streaming Visualization ────────────────────────────────
function StreamingVisualization() {
  const streams = [
    { label: "upload:", target: "S3", color: "blue" as const, delay: 0.1 },
    {
      label: "download:",
      target: "Local",
      color: "green" as const,
      delay: 0.2,
    },
    { label: "transfer:", target: "GCS", color: "purple" as const, delay: 0.3 },
  ];

  return (
    <motion.div
      initial={{ opacity: 0 }}
      whileInView={{ opacity: 1 }}
      viewport={{ once: true }}
      transition={{ duration: 0.6 }}
      className="relative"
    >
      {/* Background glow */}
      <div className="absolute inset-0 -m-6 bg-linear-to-br from-purple-500/5 via-transparent to-blue-500/5 rounded-3xl blur-xl" />

      <div className="relative p-3 sm:p-6 rounded-2xl border border-fd-border/50 bg-fd-card/30 backdrop-blur-sm">
        <div className="flex flex-col items-center gap-6">
          {/* Stream flow diagram */}
          <div className="flex flex-col gap-4 w-full">
            {streams.map((s, i) => (
              <motion.div
                key={s.label}
                initial={{ opacity: 0, x: -12 }}
                whileInView={{ opacity: 1, x: 0 }}
                viewport={{ once: true }}
                transition={{ duration: 0.4, delay: s.delay }}
                className="flex items-center gap-0"
              >
                <FlowNode
                  label={s.label}
                  color={s.color}
                  size="sm"
                  pulse={i === 0}
                  delay={s.delay}
                />
                <FlowLine length={48} color={s.color} delay={s.delay + 0.2} />
                <FlowNode
                  label={s.target}
                  color={s.color}
                  size="sm"
                  delay={s.delay + 0.3}
                />
              </motion.div>
            ))}
          </div>

          {/* Legend */}
          <div className="flex items-center gap-4 text-[10px] text-fd-muted-foreground">
            <div className="flex items-center gap-1.5">
              <div className="size-2 rounded-full bg-blue-500" />
              <span>Active</span>
            </div>
            <div className="flex items-center gap-1.5">
              <div className="size-2 rounded-full bg-green-500" />
              <span>Streaming</span>
            </div>
            <div className="flex items-center gap-1.5">
              <div className="size-2 rounded-full bg-purple-500" />
              <span>Queued</span>
            </div>
          </div>
        </div>
      </div>

      {/* Code block below visualization */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        whileInView={{ opacity: 1, y: 0 }}
        viewport={{ once: true }}
        transition={{ duration: 0.5, delay: 0.4 }}
        className="mt-4"
      >
        <CodeBlock
          code={streamCode}
          filename="stream.go"
          showLineNumbers={false}
          className="text-xs"
        />
      </motion.div>
    </motion.div>
  );
}

// ─── Streaming Showcase Section ─────────────────────────────
export function StreamingShowcase() {
  return (
    <section className="relative w-full py-20 sm:py-28 overflow-hidden">
      {/* Background */}
      <div className="absolute inset-0 bg-linear-to-b from-transparent via-purple-500/2 to-transparent" />

      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <div className="grid gap-12 lg:grid-cols-2 lg:gap-16 items-center">
          {/* Left: Visual */}
          <div className="relative">
            <StreamingVisualization />
          </div>

          {/* Right: Text content */}
          <div className="flex flex-col">
            <SectionHeader
              badge="Streaming"
              title="Stream everything."
              description="Chunked transfers with backpressure handling, pause/resume, and managed stream pools. Upload, download, and transfer large objects efficiently."
              align="left"
            />

            <div className="mt-8 space-y-5">
              <FeatureBullet
                title="Chunked Transfers"
                description="Configurable chunk sizes with buffered I/O. Default 8MB chunks with 32KB stream buffers for optimal throughput."
                delay={0.2}
              />
              <FeatureBullet
                title="Backpressure"
                description="Block, drop, or adaptive backpressure modes to prevent memory exhaustion during high-throughput transfers."
                delay={0.3}
              />
              <FeatureBullet
                title="Stream Pools"
                description="Managed concurrency with configurable pool sizes, bandwidth throttling, and lifecycle hooks for progress, chunk, and completion events."
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
                href="/docs/storage/streaming-engine"
                className="inline-flex items-center gap-1 text-sm font-medium text-blue-600 dark:text-blue-400 hover:text-blue-500 transition-colors"
              >
                Explore streaming
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
        </div>
      </div>
    </section>
  );
}
