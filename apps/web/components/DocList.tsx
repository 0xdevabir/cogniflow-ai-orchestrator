"use client";

import { useEffect, useState } from "react";

export type DocMeta = {
  id: string;
  workspace_id: string;
  title: string;
  source: string;
  mime_type: string;
  size: number;
  chunk_count: number;
  created_at: string;
};

export type DocListProps = {
  apiBase?: string;
  workspace?: string;
  refreshKey?: number;
};

export function DocList({ apiBase = "/api/proxy", workspace = "default", refreshKey }: DocListProps) {
  const [docs, setDocs] = useState<DocMeta[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`${apiBase}/docs?workspace=${encodeURIComponent(workspace)}`);
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}: ${await res.text()}`);
      }
      const data = await res.json();
      setDocs(data.documents ?? []);
    } catch (e: any) {
      setError(e?.message ?? "Failed to list docs");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace, refreshKey]);

  async function deleteDoc(id: string) {
    if (!confirm("Delete this document and its chunks?")) return;
    try {
      const res = await fetch(`${apiBase}/docs/${id}`, { method: "DELETE" });
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}: ${await res.text()}`);
      }
      await refresh();
    } catch (e: any) {
      setError(e?.message ?? "Delete failed");
    }
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <strong>📚 Workspace documents</strong>
        <span style={{ fontSize: "0.75rem", color: "#666" }}>({docs.length})</span>
        <button
          onClick={refresh}
          style={{
            marginLeft: "auto",
            background: "transparent",
            border: "1px solid var(--cf-border)",
            borderRadius: 6,
            padding: "0.2rem 0.55rem",
            fontSize: "0.75rem",
            cursor: "pointer",
          }}
        >
          ↻ Refresh
        </button>
      </div>
      {loading && <div style={{ fontSize: "0.8rem", color: "#666" }}>Loading…</div>}
      {error && (
        <div style={{ padding: "0.5rem 0.75rem", background: "#fef2f2", color: "#991b1b", borderRadius: 6, fontSize: "0.8rem" }}>
          ⚠ {error}
        </div>
      )}
      {!loading && !error && docs.length === 0 && (
        <div style={{ fontSize: "0.85rem", color: "#888", fontStyle: "italic" }}>
          No documents in this workspace yet — upload one to enable RAG retrieval.
        </div>
      )}
      {docs.map((d) => (
        <div
          key={d.id}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            padding: "0.6rem 0.75rem",
            border: "1px solid var(--cf-border)",
            borderRadius: 8,
            background: "#fff",
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 600, fontSize: "0.9rem", color: "#111827" }}>{d.title}</div>
            <div style={{ fontSize: "0.75rem", color: "#6b7280" }}>
              {d.chunk_count} chunks · {Math.max(d.size, 1)} bytes · {new Date(d.created_at).toLocaleString()}
            </div>
          </div>
          <button
            onClick={() => deleteDoc(d.id)}
            style={{
              background: "transparent",
              border: "1px solid #fca5a5",
              color: "#991b1b",
              borderRadius: 6,
              padding: "0.2rem 0.55rem",
              fontSize: "0.75rem",
              cursor: "pointer",
            }}
          >
            Delete
          </button>
        </div>
      ))}
    </div>
  );
}
