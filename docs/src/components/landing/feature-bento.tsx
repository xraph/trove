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
}

// ─── Tier 1: Hero capability cards ──────────────────────────
const heroFeatures: FeatureCard[] = [
  {
    title: "Multi-Backend Storage",
    description:
      "6 storage drivers with a unified interface. Local filesystem, S3, GCS, Azure Blob, SFTP, and in-memory — swap backends without changing application code.",
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
        <path d="M2 17l10 5 10-5" />
        <path d="M2 12l10 5 10-5" />
      </svg>
    ),
    code: `drv := localdriver.New()
drv.Open(ctx, "file:///tmp/storage")

t, _ := trove.Open(drv,
    trove.WithDefaultBucket("uploads"),
    trove.WithBackend("archive", s3drv),
    trove.WithRoute("*.log", "archive"),
)`,
    filename: "storage.go",
  },
  {
    title: "Composable Middleware",
    description:
      "Direction-aware, scope-aware middleware pipeline. Zstd compression, AES-256-GCM encryption, BLAKE3 deduplication, ClamAV scanning, and invisible watermarking.",
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
        <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
      </svg>
    ),
    code: `t, _ := trove.Open(drv,
    trove.WithMiddleware(
        compress.New(),
        encrypt.New(encrypt.WithKeyProvider(kp)),
        dedup.New(),
    ),
)`,
    filename: "middleware.go",
  },
  {
    title: "Streaming Engine",
    description:
      "Chunked transfers with configurable sizes, backpressure handling, pause/resume, and managed stream pools. Upload, download, and bidirectional streaming modes.",
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
        <path d="M4 12h4M16 12h4M12 4v4M12 16v4" />
        <circle cx="12" cy="12" r="4" />
      </svg>
    ),
    code: `s, _ := t.Stream(ctx, "media", "video.mp4",
    stream.Upload,
    stream.WithChunkSize(16 * 1024 * 1024),
    stream.OnProgress(func(p stream.Progress) {
        log.Printf("%.1f%%", p.Percent())
    }),
)
defer s.Close()`,
    filename: "stream.go",
  },
];

// ─── Tier 2: Secondary feature cards ────────────────────────
const secondaryFeatures: FeatureCard[] = [
  {
    title: "Content-Addressable Storage",
    description:
      "Store by content hash with BLAKE3, SHA-256, or XXHash. Automatic deduplication via reference counting and garbage collection support.",
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
    code: `t, _ := trove.Open(drv, trove.WithCAS(cas.BLAKE3))

ref, _ := t.CAS().Store(ctx, reader)
// ref.Hash = "blake3:a1b2c3..."

obj, _ := t.CAS().Retrieve(ctx, ref.Hash)
defer obj.Close()`,
    filename: "cas.go",
  },
  {
    title: "Virtual Filesystem",
    description:
      "io/fs.FS-compatible interface over flat object storage. Hierarchical directory view, metadata management, and standard Go file operations.",
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
        <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
      </svg>
    ),
    code: `fsys := t.VFS("uploads")
iofs := vfs.NewIOFS(ctx, fsys)

// Serve files over HTTP
http.Handle("/files",
    http.FileServer(http.FS(iofs)))`,
    filename: "vfs.go",
  },
  {
    title: "Multi-Backend Routing",
    description:
      "Route objects to backends via glob patterns or custom functions. Send logs to archive storage, images to CDN-backed S3, temp files to memory.",
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
        <rect x="3" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="3" width="7" height="7" rx="1" />
        <rect x="3" y="14" width="7" height="7" rx="1" />
        <rect x="14" y="14" width="7" height="7" rx="1" />
      </svg>
    ),
    code: `t, _ := trove.Open(drv,
    trove.WithBackend("archive", s3drv),
    trove.WithBackend("cdn", gcsDrv),
    trove.WithRoute("*.log", "archive"),
    trove.WithRoute("images/*", "cdn"),
)`,
    filename: "routing.go",
  },
  {
    title: "Capability Interfaces",
    description:
      "Opt-in driver capabilities: multipart uploads, presigned URLs, server-side copy, versioning, change notifications, and lifecycle rules.",
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
        <rect x="2" y="2" width="20" height="6" rx="1" />
        <rect x="2" y="10" width="20" height="6" rx="1" />
        <path d="M6 18h12v3a1 1 0 01-1 1H7a1 1 0 01-1-1v-3z" />
      </svg>
    ),
    code: `if mp, ok := drv.(driver.MultipartDriver); ok {
    upload, _ := mp.InitiateMultipart(ctx,
        bucket, key)
    mp.UploadPart(ctx, upload.ID, 1, part)
    mp.CompleteMultipart(ctx, upload.ID)
}`,
    filename: "capabilities.go",
  },
  {
    title: "Forge Extension",
    description:
      "First-class Forge integration with REST API, ORM models, database migrations, DI container, and ecosystem hooks for Chronicle, Dispatch, and Warden.",
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
        <path d="M2 17l10 5 10-5" />
        <path d="M2 12l10 5 10-5" />
      </svg>
    ),
    code: `app := forge.New(
    troveext.New(
        troveext.WithDriver("local", localDrv),
        troveext.WithDriver("s3", s3Drv),
        troveext.WithDefaultDriver("local"),
    ),
)`,
    filename: "extension.go",
  },
  {
    title: "Security",
    description:
      "AES-256-GCM encryption with pluggable KeyProvider for key rotation. ClamAV-powered virus scanning on write. Invisible watermarking for PNG, JPEG, and PDF.",
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
        <path d="M18 20V10M12 20V4M6 20v-6" />
      </svg>
    ),
    code: `t, _ := trove.Open(drv,
    trove.WithMiddleware(
        encrypt.New(
            encrypt.WithKeyProvider(vault.Provider()),
        ),
        scan.New(scan.WithClamAV(clamAddr)),
    ),
)`,
    filename: "security.go",
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

// ─── Card Component ─────────────────────────────────────────
function FeatureCardView({
  feature,
  tier,
}: {
  feature: FeatureCard;
  tier: "hero" | "secondary";
}) {
  return (
    <motion.div
      variants={itemVariants}
      className={cn(
        "group relative rounded-xl border border-fd-border bg-fd-card/50 backdrop-blur-sm p-6 hover:border-blue-500/20 hover:bg-fd-card/80 transition-all duration-300",
        tier === "hero" && "ring-1 ring-blue-500/5",
      )}
    >
      {/* Header */}
      <div className="flex items-start gap-3 mb-4">
        <div
          className={cn(
            "flex items-center justify-center shrink-0 rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400",
            tier === "hero" ? "size-10" : "size-9",
          )}
        >
          {feature.icon}
        </div>
        <div>
          <h3
            className={cn(
              "font-semibold text-fd-foreground",
              tier === "hero" ? "text-base" : "text-sm",
            )}
          >
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
  );
}

// ─── Feature Bento Section ──────────────────────────────────
export function FeatureBento() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Capabilities"
          title="Everything you need for storage"
          description="Drivers, middleware, streaming, CAS, VFS, routing, and security — Trove is a complete object storage toolkit for Go applications."
        />

        {/* Tier 1: Hero capability cards */}
        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-50px" }}
          className="mt-14 grid grid-cols-1 md:grid-cols-3 gap-4"
        >
          {heroFeatures.map((feature) => (
            <FeatureCardView
              key={feature.title}
              feature={feature}
              tier="hero"
            />
          ))}
        </motion.div>

        {/* Tier 2: Secondary feature cards */}
        <motion.div
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-50px" }}
          className="mt-4 grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4"
        >
          {secondaryFeatures.map((feature) => (
            <FeatureCardView
              key={feature.title}
              feature={feature}
              tier="secondary"
            />
          ))}
        </motion.div>
      </div>
    </section>
  );
}
