"use client";

import { useCallback, useRef, useState } from "react";

export type DocUploaderProps = {
  apiBase?: string;
  workspace?: string;
  onUploaded?: (doc: { doc_id: string; title: string; chunk_count: number }) => void;
};

export function DocUploader({
  apiBase = "/api/proxy",
  workspace = "default",
  onUploaded,
}: DocUploaderProps) {
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [lastDoc, setLastDoc] = useState<{ doc_id: string; title: string; chunk_count: number } | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const upload = useCallback(
    async (file: File) => {
      setError(null);
      setUploading(true);
      setProgress(5);
      try {
        const form = new FormData();
        form.append("file", file);
        form.append("workspace", workspace);
        form.append("title", file.name);
        setProgress(35);
        const res = await fetch(`${apiBase}/v1/docs`, {
          method: "POST",
          body: form,
        });
        setProgress(85);
        if (!res.ok) {
          const body = await res.text();
          throw new Error(`HTTP ${res.status}: ${body}`);
        }
        const data = await res.json();
        setLastDoc({ doc_id: data.doc_id, title: data.title, chunk_count: data.chunk_count });
        onUploaded?.(data);
        setProgress(100);
      } catch (e: any) {
        setError(e?.message ?? "Upload failed");
      } finally {
        setUploading(false);
        setTimeout(() => setProgress(0), 1200);
      }
    },
    [apiBase, workspace, onUploaded],
  );

  const onDrop = useCallback(
    (e: React.DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      setDragging(false);
      const file = e.dataTransfer.files?.[0];
      if (file) upload(file);
    },
    [upload],
  );

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
      onClick={() => inputRef.current?.click()}
      role="button"
      tabIndex={0}
      style={{
        border: `2px dashed ${dragging ? "var(--cf-accent)" : "var(--cf-border)"}`,
        background: dragging ? "#eef2ff" : "#fafafa",
        borderRadius: 12,
        padding: "1.5rem",
        textAlign: "center",
        cursor: "pointer",
        transition: "background 0.15s, border-color 0.15s",
      }}
    >
      <input
        ref={inputRef}
        type="file"
        accept=".pdf,.txt,.md,.markdown,text/plain,application/pdf,text/markdown"
        style={{ display: "none" }}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) upload(f);
        }}
      />
      <div style={{ fontSize: "1rem", color: "#374151", fontWeight: 600 }}>
        {uploading ? "Uploading…" : "📄 Drag a document, or click to browse"}
      </div>
      <div style={{ fontSize: "0.8rem", color: "#6b7280", marginTop: 4 }}>
        PDF, TXT, or Markdown · workspace: <code>{workspace}</code>
      </div>
      {progress > 0 && progress < 100 && (
        <div
          style={{
            marginTop: 10,
            height: 6,
            background: "#e5e7eb",
            borderRadius: 3,
            overflow: "hidden",
          }}
        >
          <div
            style={{
              width: `${progress}%`,
              height: "100%",
              background: "var(--cf-accent)",
              transition: "width 0.2s",
            }}
          />
        </div>
      )}
      {lastDoc && !uploading && (
        <div
          style={{
            marginTop: 8,
            fontSize: "0.8rem",
            color: "#166534",
          }}
        >
          ✅ Uploaded <strong>{lastDoc.title}</strong> ({lastDoc.chunk_count} chunks)
        </div>
      )}
      {error && (
        <div
          style={{
            marginTop: 8,
            fontSize: "0.8rem",
            color: "#991b1b",
          }}
        >
          ⚠ {error}
        </div>
      )}
    </div>
  );
}
