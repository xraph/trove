"use client";

import { motion } from "framer-motion";
import { CodeBlock } from "./code-block";
import { SectionHeader } from "./section-header";

const localCode = `package main

import (
  "context"
  "fmt"
  "strings"

  "github.com/xraph/trove"
  "github.com/xraph/trove/drivers/localdriver"
)

func main() {
  ctx := context.Background()

  // Create and open the driver
  drv := localdriver.New()
  drv.Open(ctx, "file:///tmp/storage")

  // Pass connected driver to Trove
  t, _ := trove.Open(drv,
    trove.WithDefaultBucket("uploads"),
  )
  defer t.Close(ctx)

  // Store an object
  t.Put(ctx, "uploads", "hello.txt",
    strings.NewReader("Hello, Trove!"))

  // Retrieve it
  obj, _ := t.Get(ctx, "uploads", "hello.txt")
  defer obj.Close()
  fmt.Printf("stored %d bytes\\n", obj.Size)
}`;

const s3Code = `package main

import (
  "context"
  "fmt"
  "strings"

  "github.com/xraph/trove"
  "github.com/xraph/trove/drivers/s3driver"
)

func main() {
  ctx := context.Background()

  // Create and open the driver
  drv := s3driver.New()
  drv.Open(ctx, "s3://us-east-1/my-bucket",
    s3driver.WithCredentials(key, secret),
  )

  // Same API, different backend
  t, _ := trove.Open(drv,
    trove.WithDefaultBucket("my-bucket"),
  )
  defer t.Close(ctx)

  // Store an object
  t.Put(ctx, "my-bucket", "hello.txt",
    strings.NewReader("Hello, Trove!"))

  // Retrieve it
  obj, _ := t.Get(ctx, "my-bucket", "hello.txt")
  defer obj.Close()
  fmt.Printf("stored %d bytes\\n", obj.Size)
}`;

export function CodeShowcase() {
  return (
    <section className="relative w-full py-20 sm:py-28">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        <SectionHeader
          badge="Developer Experience"
          title="Same API. Any backend."
          description="Write storage code that works with local filesystem and deploy to S3 without changing a single line. Trove's unified interface means your code is backend-agnostic."
        />

        <div className="mt-14 grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Local Filesystem side */}
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-blue-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Local Filesystem
              </span>
            </div>
            <CodeBlock code={localCode} filename="local.go" />
          </motion.div>

          {/* Amazon S3 side */}
          <motion.div
            initial={{ opacity: 0, x: 20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.2 }}
          >
            <div className="mb-3 flex items-center gap-2">
              <div className="size-2 rounded-full bg-indigo-500" />
              <span className="text-xs font-medium text-fd-muted-foreground uppercase tracking-wider">
                Amazon S3
              </span>
            </div>
            <CodeBlock code={s3Code} filename="s3.go" />
          </motion.div>
        </div>
      </div>
    </section>
  );
}
