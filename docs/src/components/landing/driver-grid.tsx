"use client";

import { motion } from "framer-motion";
import { cn } from "@/lib/cn";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

// ─── Corner decorators ──────────────────────────────────────
function CardDecorator() {
  return (
    <>
      <span className="border-blue-500/40 absolute -left-px -top-px block size-2 border-l-2 border-t-2" />
      <span className="border-blue-500/40 absolute -right-px -top-px block size-2 border-r-2 border-t-2" />
      <span className="border-blue-500/40 absolute -bottom-px -left-px block size-2 border-b-2 border-l-2" />
      <span className="border-blue-500/40 absolute -bottom-px -right-px block size-2 border-b-2 border-r-2" />
    </>
  );
}

// ─── Data ───────────────────────────────────────────────────

interface FeaturedDriver {
  abbr: string;
  name: string;
  description: string;
  color: string;
  code: string;
  filename: string;
}

interface CompactDriver {
  abbr: string;
  name: string;
  description: string;
  color: string;
}

const featuredDrivers: FeaturedDriver[] = [
  {
    abbr: "FS",
    name: "Local Filesystem",
    description:
      "Atomic file operations with sidecar .meta.json metadata. Directory-as-bucket layout with configurable base paths.",
    color: "blue",
    code: `drv := localdriver.New()
drv.Open(ctx, "file:///tmp/storage")

t, _ := trove.Open(drv)

// Store an object
t.Put(ctx, "uploads", "photo.jpg", reader)

// Retrieve it
obj, _ := t.Get(ctx, "uploads", "photo.jpg")
defer obj.Close()`,
    filename: "local.go",
  },
  {
    abbr: "S3",
    name: "S3 / MinIO / R2",
    description:
      "S3-compatible storage with multipart uploads, presigned URLs, server-side copy, and range reads.",
    color: "orange",
    code: `drv := s3driver.New()
drv.Open(ctx, "s3://us-east-1/my-bucket",
    s3driver.WithCredentials(key, secret),
)

t, _ := trove.Open(drv)
t.Put(ctx, "my-bucket", "data.csv", reader)`,
    filename: "s3.go",
  },
  {
    abbr: "GC",
    name: "Google Cloud Storage",
    description:
      "GCS with resumable uploads, presigned URLs, and compose-based multipart. Native IAM integration.",
    color: "green",
    code: `drv := gcsdriver.New()
drv.Open(ctx, "gcs://my-project/my-bucket",
    gcsdriver.WithCredentialsFile("sa.json"),
)

t, _ := trove.Open(drv)
t.Put(ctx, "my-bucket", "report.pdf", reader)`,
    filename: "gcs.go",
  },
];

const compactDrivers: CompactDriver[] = [
  {
    abbr: "AZ",
    name: "Azure Blob",
    description: "Block blob storage with SAS tokens and multipart",
    color: "purple",
  },
  {
    abbr: "SF",
    name: "SFTP",
    description: "Remote file storage over SSH",
    color: "teal",
  },
  {
    abbr: "MM",
    name: "In-Memory",
    description: "Ephemeral storage for testing and caching",
    color: "amber",
  },
];

const unifiedCode = `// One interface. Any backend. Zero changes.
local := localdriver.New()
local.Open(ctx, "file:///tmp/storage")

s3drv := s3driver.New()
s3drv.Open(ctx, "s3://us-east-1/my-bucket")

// Same trove.Open — same API for every backend
localStore, _ := trove.Open(local)
s3Store, _ := trove.Open(s3drv)`;

// ─── Color maps ─────────────────────────────────────────────

const badgeColorMap: Record<string, string> = {
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20",
  orange:
    "bg-orange-500/10 text-orange-600 dark:text-orange-400 border-orange-500/20",
  green:
    "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20",
  teal: "bg-teal-500/10 text-teal-600 dark:text-teal-400 border-teal-500/20",
  purple:
    "bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20",
  amber:
    "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
  indigo:
    "bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 border-indigo-500/20",
};

// ─── Animation ──────────────────────────────────────────────

const containerVariants = {
  hidden: {},
  visible: {
    transition: { staggerChildren: 0.08 },
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

// ─── Featured Driver Card ───────────────────────────────────

function FeaturedCard({ driver }: { driver: FeaturedDriver }) {
  return (
    <motion.div
      variants={itemVariants}
      className="group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm overflow-hidden hover:border-blue-500/20 transition-all duration-300"
    >
      <CardDecorator />

      <div className="p-5 pb-3">
        <div className="flex items-center gap-3 mb-3">
          <div
            className={cn(
              "flex items-center justify-center size-9 rounded-lg font-mono text-xs font-bold border",
              badgeColorMap[driver.color],
            )}
          >
            {driver.abbr}
          </div>
          <div>
            <h3 className="text-sm font-semibold text-fd-foreground">
              {driver.name}
            </h3>
          </div>
        </div>
        <p className="text-xs text-fd-muted-foreground leading-relaxed">
          {driver.description}
        </p>
      </div>

      <div className="px-5 pb-5">
        <CodeBlock
          code={driver.code}
          filename={driver.filename}
          showLineNumbers={false}
          className="text-xs"
        />
      </div>
    </motion.div>
  );
}

// ─── Compact Driver Card ────────────────────────────────────

function CompactCard({ driver }: { driver: CompactDriver }) {
  return (
    <motion.div
      variants={itemVariants}
      className="group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm p-4 hover:border-blue-500/20 transition-all duration-300"
    >
      <div className="flex items-center gap-3">
        <div
          className={cn(
            "flex items-center justify-center size-8 rounded-lg font-mono text-[10px] font-bold border shrink-0",
            badgeColorMap[driver.color],
          )}
        >
          {driver.abbr}
        </div>
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-fd-foreground">
            {driver.name}
          </h3>
          <p className="text-[11px] text-fd-muted-foreground leading-snug mt-0.5">
            {driver.description}
          </p>
        </div>
      </div>
    </motion.div>
  );
}

// ─── Unified API Card ───────────────────────────────────────

function UnifiedAPICard() {
  return (
    <motion.div
      variants={itemVariants}
      className="group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm overflow-hidden lg:col-span-2 hover:border-blue-500/20 transition-all duration-300"
    >
      <CardDecorator />

      <div className="grid lg:grid-cols-[1fr_1.2fr] gap-0">
        {/* Left: text */}
        <div className="p-6 flex flex-col justify-center">
          <div className="flex items-center gap-2 mb-3">
            <div className="flex items-center justify-center size-9 rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400">
              <svg
                className="size-4"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M12 2L2 7l10 5 10-5-10-5z" />
                <path d="M2 17l10 5 10-5" />
                <path d="M2 12l10 5 10-5" />
              </svg>
            </div>
            <h3 className="text-base font-semibold text-fd-foreground">
              One API
            </h3>
          </div>
          <p className="text-sm text-fd-muted-foreground leading-relaxed">
            Every driver plugs into the same{" "}
            <code className="text-xs bg-fd-muted/50 px-1.5 py-0.5 rounded font-mono">
              trove.Open(drv)
            </code>{" "}
            interface. Switch from local filesystem to S3 without changing
            application code. Middleware, routing, and streaming compose across
            all 6 backends.
          </p>

          <div className="mt-4 flex flex-wrap gap-2">
            {[...featuredDrivers, ...compactDrivers].map((d) => (
              <span
                key={d.abbr}
                className={cn(
                  "inline-flex items-center px-2 py-0.5 rounded text-[10px] font-mono font-medium border",
                  badgeColorMap[d.color],
                )}
              >
                {d.name}
              </span>
            ))}
          </div>
        </div>

        {/* Right: code */}
        <div className="border-t lg:border-t-0 lg:border-l border-fd-border p-5 flex items-center">
          <CodeBlock
            code={unifiedCode}
            filename="main.go"
            showLineNumbers={false}
            className="text-xs w-full"
          />
        </div>
      </div>
    </motion.div>
  );
}

// ─── Main Component ─────────────────────────────────────────

export function DriverGrid() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Drivers"
          title="6 backends. One API."
          description="Each driver implements the same storage interface. Local filesystem uses atomic file operations, S3 uses multipart uploads, GCS uses resumable uploads — no unified abstraction compromising capabilities."
        />

        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-50px" }}
          className="mt-14 grid grid-cols-1 lg:grid-cols-2 gap-4"
        >
          {/* Row 1: Local + S3 */}
          <FeaturedCard driver={featuredDrivers[0]} />
          <FeaturedCard driver={featuredDrivers[1]} />

          {/* Row 2: GCS (left) + 3 compact drivers (right) */}
          <FeaturedCard driver={featuredDrivers[2]} />
          <div className="grid grid-cols-1 sm:grid-cols-3 lg:grid-cols-1 xl:grid-cols-3 gap-4 content-start">
            {compactDrivers.map((d) => (
              <CompactCard key={d.abbr} driver={d} />
            ))}
          </div>

          {/* Row 3: Full-width unified API card */}
          <UnifiedAPICard />
        </motion.div>
      </div>
    </section>
  );
}
