"use client";

import { useRef, useState } from "react";
import {
  streamRun,
  type DowngradeEvent,
  type EvalEvent,
  type SSEEvent,
  type VerdictEvent,
  type ManifestEvent,
  type Chunk,
} from "@/lib/sse";
import { FusionViewer, type Manifest } from "@/components/FusionViewer";
import { DisagreementCard } from "@/components/DisagreementCard";
import type { Plan, NodeStatus } from "@/components/DAGCanvas";

export type FusionRunnerProps = {
  plan: Plan;
  apiBase?: string;
  onStreamEvent?: (ev: SSEEvent) => void;
  onDowngrade?: (ev: DowngradeEvent) => void;
  onEval?: (ev: EvalEvent) => void;
  onClose?: () => void;
  onJumpToNode?: (nodeId: string) => void;
  workspace?: string;
  budget?: { max_cost_usd?: number; max_carbon_g?: number } | null;
  evalEnabled?: boolean;
};

export function FusionRunner({
  plan,
  apiBase,
  onStreamEvent,
  onDowngrade,
  onEval,
  onClose,
  onJumpToNode,
  workspace,
  budget,
  evalEnabled,
}: FusionRunnerProps) {
  const [running, setRunning] = useState(false);
  const [fusionText, setFusionText] = useState("");
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [verdicts, setVerdicts] = useState<VerdictEvent[]>([]);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [fusionStarted, setFusionStarted] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);

  async function handleRun() {
    if (running) return;
    setRunning(true);
    setFusionText("");
    setManifest(null);
    setVerdicts([]);
    setErrorMsg(null);
    setFusionStarted(false);

    const controller = new AbortController();
    controllerRef.current = controller;

    try {
      for await (const ev of streamRun(plan, {
        apiBase,
        signal: controller.signal,
        parallelism: 4,
        workspace,
        budget: budget ?? undefined,
        eval: evalEnabled,
      })) {
        onStreamEvent?.(ev);
        switch (ev.event) {
          case "fusion_start":
            setFusionStarted(true);
            break;
          case "fusion":
            setFusionText((prev) => prev + (ev.data as Chunk).text);
            break;
          case "manifest": {
            const m = ev.data as ManifestEvent;
            setManifest({ v: m.v, spans: m.spans });
            break;
          }
          case "verdict":
            setVerdicts((prev) => [...prev, ev.data]);
            break;
          case "downgrade":
            onDowngrade?.(ev.data);
            break;
          case "eval":
            onEval?.(ev.data);
            break;
          case "error":
            setErrorMsg(ev.data.message);
            break;
          default:
            break;
        }
      }
    } catch (err: any) {
      if (err?.name !== "AbortError") {
        setErrorMsg(err?.message ?? "Unknown error");
      }
    } finally {
      setRunning(false);
      controllerRef.current = null;
    }
  }

  function handleCancel() {
    controllerRef.current?.abort();
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {!running && !fusionStarted && (
        <button
          onClick={handleRun}
          style={{
            padding: "0.6rem 1.2rem",
            background: "var(--cf-accent)",
            color: "#fff",
            border: "none",
            borderRadius: 8,
            cursor: "pointer",
            fontWeight: 600,
            alignSelf: "flex-start",
          }}
        >
          ⚡ Run + Fuse
        </button>
      )}

      {running && (
        <div
          style={{
            padding: "0.75rem 1rem",
            background: "#eff6ff",
            border: "1px solid #bfdbfe",
            borderRadius: 8,
            color: "#1d4ed8",
            display: "flex",
            alignItems: "center",
            gap: 8,
          }}
        >
          <span style={{ animation: "pulse 1.4s ease-in-out infinite" }}>⏳</span>
          Running DAG + fusing streams…
          <button
            onClick={handleCancel}
            style={{
              marginLeft: "auto",
              padding: "0.3rem 0.7rem",
              background: "#ef4444",
              color: "#fff",
              border: "none",
              borderRadius: 6,
              cursor: "pointer",
              fontSize: "0.8rem",
            }}
          >
            Cancel
          </button>
        </div>
      )}

      {verdicts.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {verdicts.map((v, i) => (
            <DisagreementCard
              key={i}
              verdict={v}
              onJumpToNode={onJumpToNode}
            />
          ))}
        </div>
      )}

      {fusionText && (
        <FusionViewer
          text={fusionText}
          manifest={manifest}
          onJumpToNode={onJumpToNode}
        />
      )}

      {!running && fusionStarted && (
        <div
          style={{
            display: "flex",
            gap: 8,
            fontSize: "0.85rem",
            color: "#666",
          }}
        >
          {verdicts.length === 0 && (
            <span>✅ No disagreements detected across upstream streams.</span>
          )}
          {manifest && (
            <span>
              {manifest.spans.length} citation span
              {manifest.spans.length === 1 ? "" : "s"} anchored.
            </span>
          )}
          {onClose && (
            <button
              onClick={onClose}
              style={{
                marginLeft: "auto",
                background: "transparent",
                border: "1px solid var(--cf-border)",
                borderRadius: 6,
                padding: "0.3rem 0.7rem",
                cursor: "pointer",
                fontSize: "0.8rem",
              }}
            >
              Hide
            </button>
          )}
        </div>
      )}

      {errorMsg && (
        <div
          style={{
            padding: "0.75rem",
            borderRadius: 8,
            background: "#fef2f2",
            color: "#991b1b",
            border: "1px solid #fecaca",
          }}
        >
          ⚠ {errorMsg}
        </div>
      )}
    </div>
  );
}
