"use client";

import { useState } from "react";

// --- types ---

export type Span = {
  id: string;
  sub_task_id: string;
  model: string;
  text: string;
  doc_id?: string;
  doc_snippet?: string;
  prompt_hash?: string;
  char_start?: number;
  char_end?: number;
};

export type Manifest = {
  v: "citation.v1";
  spans: Span[];
};

// --- parser ---

// Tokenize the fusion text into "text" + "cite" segments. The cite depth
// mirrors the [n] marker so we can render [1] with the same nesting level.
type Segment =
  | { kind: "text"; text: string }
  | { kind: "cite"; index: number };

export function tokenizeFusion(text: string): Segment[] {
  const out: Segment[] = [];
  let i = 0;
  let buf = "";
  while (i < text.length) {
    if (text[i] === "[") {
      // Look ahead: [n] or [n1,n2,...]
      const close = text.indexOf("]", i);
      if (close > i) {
        const inner = text.slice(i + 1, close);
        if (/^\d+(?:,\d+)*$/.test(inner)) {
          if (buf) {
            out.push({ kind: "text", text: buf });
            buf = "";
          }
          // Emit one cite per number.
          for (const part of inner.split(",")) {
            const n = parseInt(part, 10);
            if (!isNaN(n)) out.push({ kind: "cite", index: n });
          }
          i = close + 1;
          continue;
        }
      }
    }
    buf += text[i];
    i++;
  }
  if (buf) out.push({ kind: "text", text: buf });
  return out;
}

// --- component ---

export type FusionViewerProps = {
  text: string;
  manifest: Manifest | null;
  onJumpToNode?: (nodeId: string) => void;
};

export function FusionViewer({ text, manifest, onJumpToNode }: FusionViewerProps) {
  const [hovered, setHovered] = useState<number | null>(null);
  const segs = tokenizeFusion(text || "");

  // Lookup span by 1-based index.
  const spanByIndex = (n: number): Span | null => {
    if (!manifest) return null;
    // The synthesis spans are assigned in node-id order; [1] is the first
    // upstream node, [2] the second, etc.
    const spans = manifest.spans;
    if (n < 1 || n > spans.length) return null;
    return spans[n - 1];
  };

  return (
    <div
      style={{
        background: "#fff",
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        padding: "1.25rem",
        position: "relative",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <strong>🪪 Fused Answer</strong>
        {manifest && (
          <span style={{ fontSize: "0.75rem", color: "#666" }}>
            {manifest.spans.length} span{manifest.spans.length === 1 ? "" : "s"}
          </span>
        )}
      </div>
      <div
        style={{
          fontSize: "0.95rem",
          lineHeight: 1.65,
          whiteSpace: "pre-wrap",
          color: "#1f2937",
        }}
      >
        {segs.map((seg, i) => {
          if (seg.kind === "text") return <span key={i}>{seg.text}</span>;
          const span = spanByIndex(seg.index);
          if (!span) {
            return (
              <span
                key={i}
                style={{
                  background: "#fee2e2",
                  color: "#991b1b",
                  padding: "0 4px",
                  borderRadius: 3,
                  fontSize: "0.75em",
                  fontFamily: "ui-monospace, monospace",
                }}
                title="Citation index out of range"
              >
                [{seg.index}]
              </span>
            );
          }
          return (
            <span
              key={i}
              onMouseEnter={() => setHovered(seg.index)}
              onMouseLeave={() => setHovered(null)}
              onClick={() => onJumpToNode?.(span.sub_task_id)}
              style={{
                position: "relative",
                display: "inline-block",
                background: "#eef2ff",
                color: "#3730a3",
                padding: "0 4px",
                margin: "0 1px",
                borderRadius: 3,
                fontSize: "0.75em",
                fontFamily: "ui-monospace, monospace",
                fontWeight: 600,
                cursor: "pointer",
                verticalAlign: "super",
                lineHeight: 1,
              }}
              title={`Jump to ${span.sub_task_id}`}
            >
              [{seg.index}]
              {hovered === seg.index && (
                <span
                  style={{
                    position: "absolute",
                    top: "1.4em",
                    left: 0,
                    width: 280,
                    background: "#1f2937",
                    color: "#f9fafb",
                    padding: "0.6rem 0.7rem",
                    borderRadius: 6,
                    fontSize: "0.78rem",
                    fontWeight: 400,
                    fontFamily: "ui-monospace, monospace",
                    whiteSpace: "normal",
                    zIndex: 100,
                    boxShadow: "0 8px 24px rgba(0,0,0,0.2)",
                    textAlign: "left",
                  }}
                >
                  <div style={{ marginBottom: 4 }}>
                    <span style={{ color: "#9ca3af" }}>model</span>{" "}
                    <span style={{ color: "#a5f3fc" }}>{span.model}</span>
                  </div>
                  <div style={{ marginBottom: 4 }}>
                    <span style={{ color: "#9ca3af" }}>node</span>{" "}
                    <span style={{ color: "#fde68a" }}>{span.sub_task_id}</span>
                  </div>
                  {span.prompt_hash && (
                    <div style={{ marginBottom: 4 }}>
                      <span style={{ color: "#9ca3af" }}>prompt</span>{" "}
                      <span style={{ color: "#bef264" }}>{span.prompt_hash}</span>
                    </div>
                  )}
                  {span.doc_id && (
                    <div style={{ marginBottom: 4 }}>
                      <span style={{ color: "#9ca3af" }}>doc</span>{" "}
                      <span style={{ color: "#fda4af" }}>{span.doc_id}</span>
                    </div>
                  )}
                  {span.text && (
                    <div
                      style={{
                        marginTop: 4,
                        paddingTop: 4,
                        borderTop: "1px solid #374151",
                        color: "#d1d5db",
                        maxHeight: 80,
                        overflow: "hidden",
                      }}
                    >
                      {span.text.length > 160
                        ? span.text.slice(0, 160) + "…"
                        : span.text}
                    </div>
                  )}
                </span>
              )}
            </span>
          );
        })}
      </div>
    </div>
  );
}
