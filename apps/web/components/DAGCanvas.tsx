"use client";

import { useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
  Handle,
  Position,
  ReactFlowProvider,
} from "@xyflow/react";
import dagre from "dagre";
import "@xyflow/react/dist/style.css";

// --- types ---

export type PlanNode = {
  id: string;
  role: string;
  payload: string;
  status: "pending" | "running" | "ok" | "error" | "debating";
  model?: string;
  score?: number;
  breakdown?: Record<string, number>;
  reason?: string;
  task_class?: string;
  needs_rag?: boolean;
};

export type PlanEdge = { from: string; to: string };

export type Plan = {
  version: string;
  nodes: PlanNode[];
  edges: PlanEdge[];
};

export type DAGCanvasProps = {
  plan: Plan;
  onNodeClick?: (nodeId: string) => void;
};

// --- layout ---

const NODE_WIDTH = 250;
const NODE_HEIGHT = 140;

function layoutWithDagre(nodes: Node[], edges: Edge[]): { nodes: Node[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", nodesep: 60, ranksep: 140 });

  nodes.forEach((n) => g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT }));
  edges.forEach((e) => g.setEdge(e.source, e.target));

  dagre.layout(g);

  const laidOutNodes = nodes.map((n) => {
    const pos = g.node(n.id);
    return {
      ...n,
      position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    };
  });

  return { nodes: laidOutNodes, edges };
}

// --- node card ---

function PlanNodeCard({ data }: NodeProps) {
  const d = data as { plan: PlanNode };
  const p = d.plan;

  return (
    <div
      style={{
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
        background: statusColor(p.status).bg,
        border: `2px solid ${statusColor(p.status).border}`,
        borderRadius: 10,
        padding: "10px 12px",
        display: "flex",
        flexDirection: "column",
        gap: 4,
        boxShadow: "0 1px 2px rgba(0,0,0,0.05)",
        cursor: "pointer",
      }}
    >
      <Handle type="target" position={Position.Left} style={{ background: "#888" }} />
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 6,
        }}
      >
        <strong style={{ fontSize: "0.85rem" }}>{p.id}</strong>
        <span
          style={{
            fontSize: "0.65rem",
            textTransform: "uppercase",
            padding: "2px 6px",
            borderRadius: 999,
            background: statusColor(p.status).pill,
            color: statusColor(p.status).pillFg,
            fontWeight: 700,
            letterSpacing: 0.4,
          }}
        >
          {p.status}
        </span>
      </div>
      <div style={{ fontSize: "0.7rem", color: "#666" }}>{p.role}</div>
      <div
        style={{
          fontSize: "0.78rem",
          color: "#333",
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
        }}
        title={p.payload}
      >
        {p.payload}
      </div>
      {p.model && (
        <div style={{ fontSize: "0.7rem", color: "#555", marginTop: "auto" }}>
          🧠 <code style={{ fontSize: "0.7rem" }}>{p.model}</code>
          {typeof p.score === "number" && (
            <span
              title="Router score"
              style={{
                marginLeft: 6,
                background: "#eef2ff",
                color: "#4338ca",
                padding: "1px 6px",
                borderRadius: 999,
                fontFamily: "ui-monospace, monospace",
                fontSize: "0.65rem",
                fontWeight: 600,
              }}
            >
              {p.score.toFixed(2)}
            </span>
          )}
          {p.task_class && (
            <span
              style={{
                marginLeft: 4,
                background: "#f4f4f5",
                color: "#3f3f46",
                padding: "1px 6px",
                borderRadius: 999,
                fontSize: "0.6rem",
                textTransform: "uppercase",
              }}
            >
              {p.task_class}
            </span>
          )}
          {p.needs_rag && <span style={{ marginLeft: 4 }}>📚</span>}
        </div>
      )}
      <Handle type="source" position={Position.Right} style={{ background: "#888" }} />
    </div>
  );
}

const nodeTypes = { planNode: PlanNodeCard };

function statusColor(s: PlanNode["status"]): {
  bg: string;
  border: string;
  pill: string;
  pillFg: string;
} {
  switch (s) {
    case "running":
      return { bg: "#eff6ff", border: "#3b82f6", pill: "#dbeafe", pillFg: "#1d4ed8" };
    case "ok":
      return { bg: "#f0fdf4", border: "#22c55e", pill: "#dcfce7", pillFg: "#166534" };
    case "error":
      return { bg: "#fef2f2", border: "#ef4444", pill: "#fee2e2", pillFg: "#991b1b" };
    case "debating":
      return { bg: "#fff7ed", border: "#f97316", pill: "#ffedd5", pillFg: "#9a3412" };
    default:
      return { bg: "#fafafa", border: "#d4d4d8", pill: "#f4f4f5", pillFg: "#52525b" };
  }
}

// --- main ---

export function DAGCanvas({ plan, onNodeClick }: DAGCanvasProps) {
  const { nodes, edges } = useMemo(() => {
    const initialNodes: Node[] = plan.nodes.map((n) => ({
      id: n.id,
      type: "planNode",
      data: { plan: n },
      position: { x: 0, y: 0 },
    }));
    const initialEdges: Edge[] = plan.edges.map((e, i) => ({
      id: `e${i}`,
      source: e.from,
      target: e.to,
      type: "smoothstep",
      animated: plan.nodes.find((n) => n.id === e.from)?.status === "ok",
      markerEnd: { type: MarkerType.ArrowClosed },
      style: { stroke: "#94a3b8", strokeWidth: 1.6 },
    }));
    return layoutWithDagre(initialNodes, initialEdges);
  }, [plan]);

  return (
    <div
      style={{
        width: "100%",
        height: 460,
        border: "1px solid var(--cf-border)",
        borderRadius: 12,
        background: "#fff",
      }}
    >
      <ReactFlowProvider>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.18 }}
          minZoom={0.4}
          maxZoom={1.6}
          onNodeClick={(_, n) => onNodeClick?.(n.id)}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={24} color="#eef2f7" />
          <Controls showInteractive={false} />
        </ReactFlow>
      </ReactFlowProvider>
    </div>
  );
}